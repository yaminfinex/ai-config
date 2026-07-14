package grokbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/hcombin"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

const (
	pageSize                       = 20
	defaultIdentityRefreshInterval = 15 * time.Minute
)

type BinderConfig struct {
	Seat            string
	StateDir        string
	HcomBin         string
	HcomDir         string
	BusName         string
	Wait            time.Duration
	SessionEvents   string
	NudgeAfter      time.Duration
	MaxNudges       int
	SessionID       string
	IdentityRefresh time.Duration
}

type Binder struct {
	cfg         BinderConfig
	journal     *Journal
	generation  uint64
	lock        *os.File
	listener    net.Listener
	socket      string
	mu          sync.Mutex
	taps        map[net.Conn]struct{}
	drainMu     sync.Mutex
	capMu       sync.Mutex
	identityMu  sync.Mutex
	retiring    atomic.Bool
	retireOnce  sync.Once
	retired     chan struct{}
	afterAppend func(int, Receipt) error
}

func SeatDir(stateDir, seat string) string { return filepath.Join(stateDir, "grok", seat) }
func SocketPath(stateDir, seat string) string {
	return filepath.Join(SeatDir(stateDir, seat), "bridge.sock")
}

func OpenBinder(cfg BinderConfig) (*Binder, error) {
	if cfg.Seat == "" || strings.ContainsAny(cfg.Seat, "/\\\x00") {
		return nil, errors.New("seat must be a non-empty path-safe identifier")
	}
	if cfg.StateDir == "" {
		return nil, errors.New("state directory is required; set HERDER_STATE_DIR or pass --state-dir")
	}
	if cfg.HcomBin == "" {
		return nil, errors.New("real hcom binary is required; install hcom or pass --hcom-bin")
	}
	hcomPath, _, err := hcombin.ResolveExecPath(cfg.HcomBin)
	if err != nil {
		return nil, fmt.Errorf("resolve hcom binary: %w", err)
	}
	if st, statErr := os.Stat(hcomPath); statErr != nil || st.IsDir() || st.Mode()&0o111 == 0 {
		return nil, fmt.Errorf("hcom binary %s is not executable; pass the resolved real hcom 0.7.23 binary", hcomPath)
	}
	cfg.HcomBin = hcomPath
	if cfg.Wait <= 0 {
		cfg.Wait = 60 * time.Second
	}
	if cfg.NudgeAfter <= 0 {
		cfg.NudgeAfter = 30 * time.Second
	}
	if cfg.MaxNudges <= 0 {
		cfg.MaxNudges = 2
	}
	if cfg.IdentityRefresh <= 0 {
		cfg.IdentityRefresh = defaultIdentityRefreshInterval
	}
	dir := SeatDir(cfg.StateDir, cfg.Seat)
	socket := SocketPath(cfg.StateDir, cfg.Seat)
	if len(socket) >= 108 {
		return nil, fmt.Errorf("seat bridge socket path is %d bytes, but Unix sockets require fewer than 108; shorten --state-dir or the seat identifier", len(socket))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lf, err := os.OpenFile(filepath.Join(dir, "bridge.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lf.Close()
		return nil, fmt.Errorf("seat bridge is already running; connect to the existing bridge or wait for it to exit")
	}
	j, err := OpenJournal(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
		lf.Close()
		return nil, err
	}
	gen, err := j.AdvanceGeneration()
	if err != nil {
		j.Close()
		syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
		lf.Close()
		return nil, err
	}
	b := &Binder{cfg: cfg, journal: j, generation: gen, lock: lf, socket: socket, taps: make(map[net.Conn]struct{}), retired: make(chan struct{})}
	if err := writeAtomic(filepath.Join(dir, "hcom-bin"), []byte(cfg.HcomBin+"\n"), 0o600); err != nil {
		b.Close()
		return nil, err
	}
	return b, nil
}

func (b *Binder) Close() error {
	if b.listener != nil {
		b.listener.Close()
	}
	b.mu.Lock()
	for c := range b.taps {
		c.Close()
	}
	b.taps = map[net.Conn]struct{}{}
	b.mu.Unlock()
	os.Remove(b.socket)
	err := b.journal.Close()
	syscall.Flock(int(b.lock.Fd()), syscall.LOCK_UN)
	b.lock.Close()
	return err
}

func (b *Binder) Serve(ctx context.Context) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	name, err := b.bindIdentity(ctx)
	if err != nil {
		return err
	}
	b.cfg.BusName = name
	if err := os.Remove(b.socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", b.socket)
	if err != nil {
		return err
	}
	b.listener = ln
	if err := os.Chmod(b.socket, 0o600); err != nil {
		return err
	}
	go func() { <-serveCtx.Done(); ln.Close() }()
	errch := make(chan error, 4)
	go func() { errch <- b.acceptLoop(serveCtx) }()
	go func() { errch <- b.pickupLoop(serveCtx) }()
	go func() { errch <- b.identityLoop(serveCtx) }()
	if b.cfg.SessionEvents != "" {
		go func() { errch <- b.nudgeLoop(serveCtx) }()
	}
	select {
	case <-b.retired:
		cancel()
		return errSeatRetired
	case err = <-errch:
	}
	if serveCtx.Err() != nil {
		return nil
	}
	return err
}

var errSeatRetired = errors.New("Grok seat retired")

func (b *Binder) bindIdentity(ctx context.Context) (string, error) {
	namePath := filepath.Join(SeatDir(b.cfg.StateDir, b.cfg.Seat), "bus-name")
	name := b.cfg.BusName
	if name == "" {
		if data, err := os.ReadFile(namePath); err == nil {
			name = strings.TrimSpace(string(data))
		}
	}
	args := []string{"start"}
	if name != "" {
		args = append(args, "--as", name)
	}
	out, err := b.runHcomSeatIdentity(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("bind hcom identity: %w", err)
	}
	if name == "" {
		m := regexp.MustCompile(`(?m)^\[hcom:([A-Za-z0-9-]+)\]`).FindStringSubmatch(out)
		if len(m) != 2 {
			return "", errors.New("hcom start did not report an identity; run hcom start on an isolated bus to verify the installed version")
		}
		name = m[1]
	}
	if err := writeAtomic(namePath, []byte(name+"\n"), 0o600); err != nil {
		return "", err
	}
	// hcom start creates a process-bound identity as an inactive launch
	// placeholder. Grok has no vendor hook that later replaces that placeholder,
	// so an observer would otherwise age it into launch_failed even while this
	// bridge remains live. An identified exact-row JSON list is the smallest
	// read-only hcom command: it stabilizes the existing process-bound row without
	// delivering or acknowledging pending messages, and proves the durable row is
	// still present. Keep the bus-name durable before this call so a retry reclaims
	// the same identity rather than minting another one.
	if err := b.verifyIdentity(ctx, name); err != nil {
		return "", fmt.Errorf("stabilize hcom identity: %w", err)
	}
	return name, nil
}

func (b *Binder) verifyIdentity(ctx context.Context, name string) error {
	out, err := b.runHcomSeatIdentity(ctx, "list", name, "--name", name, "--json")
	if err != nil {
		return fmt.Errorf("verify hcom row %s: %w", name, err)
	}
	var row struct {
		Name string `json:"name"`
	}
	if err = json.Unmarshal([]byte(strings.TrimSpace(out)), &row); err != nil {
		return fmt.Errorf("verify hcom row %s JSON: %w", name, err)
	}
	if row.Name != name {
		return fmt.Errorf("verify hcom row %s: exact query returned %q", name, row.Name)
	}
	return nil
}

// refreshIdentity treats exact hcom row presence as the bus-liveness authority.
// The identified read refreshes hcom's status clock before its one-hour reaper
// window. If the row is already gone, the durable name is reclaimed under the
// bridge-owned process identity instead of minting a replacement coordinate.
func (b *Binder) refreshIdentity(ctx context.Context) (bool, error) {
	b.identityMu.Lock()
	defer b.identityMu.Unlock()
	if b.retiring.Load() {
		return false, errors.New("refresh hcom identity: Grok seat is retiring")
	}
	name := b.cfg.BusName
	if name == "" {
		return false, errors.New("refresh hcom identity: bridge has no durable bus name")
	}
	presenceErr := b.verifyIdentity(ctx, name)
	if presenceErr == nil {
		return false, nil
	}
	rebound, err := b.bindIdentity(ctx)
	if err != nil {
		return false, fmt.Errorf("refresh hcom identity after row presence failure (%v): rebind %s: %w", presenceErr, name, err)
	}
	if rebound != name {
		return false, fmt.Errorf("refresh hcom identity: rebind returned %q, want durable row %q", rebound, name)
	}
	return true, nil
}

func (b *Binder) refreshAndRecover(ctx context.Context) error {
	rebound, err := b.refreshIdentity(ctx)
	if err != nil {
		return err
	}
	if !rebound {
		return nil
	}
	if err = b.Drain(ctx); err != nil {
		return fmt.Errorf("drain queued messages after hcom row rebind: %w", err)
	}
	return nil
}

func (b *Binder) identityLoop(ctx context.Context) error {
	tick := time.NewTicker(b.cfg.IdentityRefresh)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if err := b.refreshAndRecover(ctx); err != nil {
				if ctx.Err() != nil || b.retiring.Load() {
					return nil
				}
				return err
			}
		}
	}
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_RDWR, mode)
	if err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (b *Binder) acceptLoop(ctx context.Context) error {
	for {
		c, err := b.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go b.handle(c)
	}
}

func (b *Binder) handle(c net.Conn) {
	defer c.Close()
	var req Request
	if err := json.NewDecoder(bufio.NewReader(c)).Decode(&req); err != nil {
		return
	}
	if req.Op == "tap" {
		b.handleTap(c, req)
		return
	}
	resp := b.execute(req)
	if err := json.NewEncoder(c).Encode(resp); err != nil {
		return
	}
	if req.Op == "retire" && resp.OK {
		b.retireOnce.Do(func() { close(b.retired) })
	}
}

func (b *Binder) execute(req Request) Response {
	r := Response{Generation: b.generation}
	if err := b.validateSessionEvidence(req.SessionID); err != nil {
		r.Error = err.Error()
		return r
	}
	if req.Op == "handshake" {
		r.OK = true
		return r
	}
	if req.Generation != b.generation {
		r.Error = staleGeneration(req.Generation, b.generation).Error()
		return r
	}
	switch req.Op {
	case "status":
		if err := b.refreshAndRecover(context.Background()); err != nil {
			r.Error = err.Error()
			return r
		}
		pending, err := b.journal.Pending(req.Generation, false)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		b.mu.Lock()
		wake := "degraded"
		if len(b.taps) > 0 {
			wake = "armed"
		}
		b.mu.Unlock()
		r.Status = &BridgeStatus{PID: os.Getpid(), Bus: b.cfg.BusName, Wake: wake, Pending: len(pending)}
	case "retire":
		b.retiring.Store(true)
		_, err := b.journal.RetireUnacked(req.Generation)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		b.drainMu.Lock()
		_, count := b.journal.Counts()
		if err = b.publishCapabilities("down"); err != nil {
			b.recordCapabilityDiagnostic("down", err)
		}
		b.drainMu.Unlock()
		r.Retired = count
	case "pending":
		p, err := b.journal.Pending(req.Generation, true)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.Pending = make([]ReceiptView, 0, len(p))
		for _, x := range p {
			r.Pending = append(r.Pending, view(x, false))
		}
	case "fetch":
		x, err := b.journal.Fetch(req.ID, req.Generation)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		v := view(x, true)
		r.Message = &v
	case "ack":
		pendingChanged, err := b.journal.ackPendingChange(req.ID, req.Generation)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		if pendingChanged {
			if err = b.publishPendingCapability(); err != nil {
				b.recordCapabilityDiagnostic("pending", err)
			}
		}
	case "send":
		out, err := b.send(req)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.Result = out
	default:
		r.Error = fmt.Sprintf("unknown bridge operation %q; reconnect with a supported client", req.Op)
		return r
	}
	r.OK = true
	return r
}

func (b *Binder) handleTap(c net.Conn, req Request) {
	if err := b.validateSessionEvidence(req.SessionID); err != nil {
		json.NewEncoder(c).Encode(Response{Generation: b.generation, Error: err.Error()})
		return
	}
	if req.Generation != 0 && req.Generation != b.generation {
		json.NewEncoder(c).Encode(Response{Generation: b.generation, Error: staleGeneration(req.Generation, b.generation).Error()})
		return
	}
	if err := json.NewEncoder(c).Encode(Response{OK: true, Generation: b.generation}); err != nil {
		return
	}
	b.mu.Lock()
	if b.retiring.Load() {
		b.mu.Unlock()
		return
	}
	b.taps[c] = struct{}{}
	b.mu.Unlock()
	if err := b.publishCapabilities("armed"); err != nil {
		b.recordCapabilityDiagnostic("armed tap", err)
	}
	defer func() {
		b.mu.Lock()
		delete(b.taps, c)
		degraded := len(b.taps) == 0 && !b.retiring.Load()
		b.mu.Unlock()
		if degraded {
			if err := b.publishCapabilities("degraded"); err != nil {
				b.recordCapabilityDiagnostic("degraded tap", err)
			}
		}
	}()
	pending, err := b.journal.Pending(b.generation, false)
	if err != nil {
		return
	}
	if len(pending) > 0 {
		if _, err = fmt.Fprintf(c, "HCOM_RECOVER pending=%d\n", len(pending)); err != nil {
			return
		}
	}
	buf := make([]byte, 1)
	for {
		if _, err = c.Read(buf); err != nil {
			return
		}
	}
}

func (b *Binder) validateSessionEvidence(presented string) error {
	if presented == "" {
		if b.cfg.SessionID != "" {
			return errors.New("request omitted session evidence, but this bridge has an owning session; reconnect from the owning session so HERDER_GROK_SESSION_ID is present")
		}
		return nil
	}
	if b.cfg.SessionID == "" {
		return errors.New("request carries session evidence, but this bridge has no owning session; restart the bridge with --session-id before reconnecting")
	}
	if presented != b.cfg.SessionID {
		return errors.New("request session does not match this seat; reconnect through the owning session's MCP server")
	}
	return nil
}

func wakeLine(r Receipt) string {
	thread := r.Message.Thread
	if thread == "" {
		thread = "-"
	}
	intent := r.Message.Intent
	if intent == "" {
		intent = "inform"
	}
	return fmt.Sprintf("HCOM id=%d from=%s intent=%s thread=%s h=%s", r.Event.ID, r.Message.From, intent, thread, r.Hash)
}

func (b *Binder) wake(r Receipt, kind string) error {
	b.mu.Lock()
	droppedLast := false
	for c := range b.taps {
		_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if _, err := fmt.Fprintln(c, wakeLine(r)); err != nil {
			c.Close()
			delete(b.taps, c)
			droppedLast = len(b.taps) == 0
			continue
		}
		_ = c.SetWriteDeadline(time.Time{})
		if err := b.journal.Surface(r.Event.ID, kind, b.generation); err != nil {
			recordErr := fmt.Errorf("record %s surface for message %d: %w; tap dropped so reconnect recovery can re-list pending messages", kind, r.Event.ID, err)
			c.Close()
			delete(b.taps, c)
			droppedLast = len(b.taps) == 0
			if diagErr := appendDiagnostic(filepath.Join(SeatDir(b.cfg.StateDir, b.cfg.Seat), "bridge.log"), recordErr); diagErr != nil {
				b.mu.Unlock()
				return fmt.Errorf("%v; write bridge diagnostic: %w", recordErr, diagErr)
			}
			b.mu.Unlock()
			return recordErr
		}
	}
	b.mu.Unlock()
	if droppedLast && !b.retiring.Load() {
		if err := b.publishCapabilities("degraded"); err != nil {
			b.recordCapabilityDiagnostic("degraded", err)
		}
	}
	return nil
}

func (b *Binder) pickupLoop(ctx context.Context) error {
	for {
		if err := b.Drain(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		_, _ = b.events(ctx, true, b.journal.Cursor())
	}
}

func (b *Binder) Drain(ctx context.Context) error {
	b.drainMu.Lock()
	defer b.drainMu.Unlock()
	if b.retiring.Load() {
		return nil
	}
	for {
		rows, err := b.events(ctx, false, b.journal.Cursor())
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		for pageIndex, row := range rows {
			if b.retiring.Load() {
				return nil
			}
			raw, err := eventRaw(row)
			if err != nil {
				return err
			}
			r, added, err := b.queueReceipt(raw)
			if err != nil {
				return err
			}
			if added {
				if b.afterAppend != nil {
					if err := b.afterAppend(pageIndex+1, r); err != nil {
						return err
					}
				}
				if err := b.wake(r, "wake"); err != nil {
					return err
				}
			}
		}
	}
}

func (b *Binder) queueReceipt(raw json.RawMessage) (Receipt, bool, error) {
	receipt, added, pendingChanged, err := b.journal.queuePendingChange(raw)
	if err != nil {
		return Receipt{}, false, err
	}
	if pendingChanged {
		if err = b.publishPendingCapability(); err != nil {
			b.recordCapabilityDiagnostic("pending", err)
		}
	}
	return receipt, added, nil
}

func (b *Binder) publishCapabilities(wake string) error {
	switch wake {
	case "armed", "degraded", "down":
	default:
		return fmt.Errorf("refuse invalid Grok wake capability %q", wake)
	}
	if wake != "down" && b.retiring.Load() {
		return nil
	}
	b.capMu.Lock()
	defer b.capMu.Unlock()
	pending, retired := b.journal.Counts()
	capabilities := v2.Capabilities{Bus: "bound", Wake: wake, Pending: pending, BinderPID: os.Getpid(), Undeliverable: retired}
	if wake == "down" {
		capabilities.Bus = ""
		capabilities.Pending = 0
		capabilities.BinderPID = 0
	}
	return b.updateCapabilities(wake, func(*v2.SessionRecord) (v2.Capabilities, bool) {
		if wake != "down" && b.retiring.Load() {
			return v2.Capabilities{}, false
		}
		return capabilities, true
	})
}

func (b *Binder) publishPendingCapability() error {
	if b.retiring.Load() {
		return nil
	}
	b.capMu.Lock()
	defer b.capMu.Unlock()
	pending, retired := b.journal.Counts()
	b.mu.Lock()
	wake := "degraded"
	if len(b.taps) > 0 {
		wake = "armed"
	}
	b.mu.Unlock()
	return b.updateCapabilities("pending", func(latest *v2.SessionRecord) (v2.Capabilities, bool) {
		if b.retiring.Load() {
			return v2.Capabilities{}, false
		}
		if latest.Capabilities != nil {
			capabilities := *latest.Capabilities
			capabilities.Pending = pending
			return capabilities, true
		}
		return v2.Capabilities{Bus: "bound", Wake: wake, Pending: pending, BinderPID: os.Getpid(), Undeliverable: retired}, true
	})
}

func (b *Binder) updateCapabilities(label string, build func(*v2.SessionRecord) (v2.Capabilities, bool)) error {
	registryPath := filepath.Join(b.cfg.StateDir, "registry.jsonl")
	if _, err := os.Stat(registryPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect Grok registry before capability update: %w", err)
	}
	changed := false
	outcomes, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		latest := registry.V2ByGUID(tx.Projection, b.cfg.Seat)
		if latest == nil || latest.Tool != "grok" {
			return nil, nil
		}
		capabilities, ok := build(latest)
		if !ok {
			return nil, nil
		}
		if latest.Capabilities != nil && *latest.Capabilities == capabilities {
			return nil, nil
		}
		next := *latest
		next.Event = "registered"
		next.RecordedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
		next.Capabilities = &capabilities
		changed = true
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		return fmt.Errorf("record Grok %s capability: %w", label, err)
	}
	if !changed {
		return nil
	}
	outcome, err := registry.SingleOutcome(outcomes)
	if err != nil {
		return fmt.Errorf("record Grok %s capability: %w", label, err)
	}
	if err = outcome.Err(); err != nil {
		return fmt.Errorf("record Grok %s capability: %w", label, err)
	}
	if outcome.Status != registry.WriteApplied {
		return fmt.Errorf("record Grok %s capability: registry row was not appended", label)
	}
	return nil
}

func (b *Binder) recordCapabilityDiagnostic(label string, err error) {
	_ = appendDiagnostic(filepath.Join(SeatDir(b.cfg.StateDir, b.cfg.Seat), "bridge.log"), fmt.Errorf("record %s capability: %w", label, err))
}

func (b *Binder) nudgeLoop(ctx context.Context) error {
	interval := b.cfg.NudgeAfter / 2
	if interval > time.Second {
		interval = time.Second
	}
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			idle, err := sessionIdle(b.cfg.SessionEvents)
			if err != nil || !idle {
				continue
			}
			rows, err := b.journal.NudgeCandidates(b.generation, time.Now().Add(-b.cfg.NudgeAfter), b.cfg.MaxNudges)
			if err != nil {
				return err
			}
			for _, r := range rows {
				if err := b.wake(r, "nudge"); err != nil {
					return err
				}
			}
		}
	}
}

func sessionIdle(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	var last map[string]any
	for s.Scan() {
		var row map[string]any
		if json.Unmarshal(s.Bytes(), &row) == nil {
			last = row
		}
	}
	if err := s.Err(); err != nil {
		return false, err
	}
	if last == nil {
		return false, nil
	}
	event, _ := last["event"].(string)
	if event == "" {
		event, _ = last["type"].(string)
	}
	phase, _ := last["phase"].(string)
	switch event {
	case "turn_completed", "stop", "idle":
		return true, nil
	}
	switch phase {
	case "idle", "listening", "waiting_for_user":
		return true, nil
	case "waiting_for_model", "tool_execution", "permission_prompt":
		return false, nil
	}
	return false, nil
}

func sqlQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }
func (b *Binder) events(ctx context.Context, wait bool, cursor int64) ([]Event, error) {
	sql := fmt.Sprintf("id IN (SELECT id FROM events_v WHERE type='message' AND id > %d AND EXISTS (SELECT 1 FROM json_each(msg_delivered_to) WHERE value='%s') ORDER BY id ASC LIMIT %d)", cursor, sqlQuote(b.cfg.BusName), pageSize)
	args := []string{"events", "--full", "--sql", sql}
	if wait {
		sec := int(b.cfg.Wait.Seconds())
		if sec < 1 {
			sec = 1
		}
		args = append(args, "--wait", strconv.Itoa(sec))
	}
	out, err := b.runHcom(ctx, true, args...)
	if err != nil {
		var ee *exec.ExitError
		if wait && errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var rows []Event
	s := bufio.NewScanner(strings.NewReader(out))
	s.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for s.Scan() {
		line := bytes.TrimSpace(s.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("parse hcom --full row: %w", err)
		}
		ev.Raw = append(json.RawMessage(nil), line...)
		rows = append(rows, ev)
	}
	return rows, s.Err()
}

func eventRaw(ev Event) (json.RawMessage, error) {
	if len(ev.Raw) > 0 {
		return append(json.RawMessage(nil), ev.Raw...), nil
	}
	return json.Marshal(ev)
}

func (b *Binder) runHcom(ctx context.Context, anonymous bool, args ...string) (string, error) {
	env := os.Environ()
	if anonymous {
		env = scrubEnv(env, "HCOM_PROCESS_ID", "CODEX_THREAD_ID")
	}
	return b.runHcomEnv(ctx, env, args...)
}

func (b *Binder) runHcomSeatIdentity(ctx context.Context, args ...string) (string, error) {
	env := hcomSeatIdentityEnv(b.cfg.Seat)
	return b.runHcomEnv(ctx, env, args...)
}

var hcomSeatIdentityEnvAllowlist = [...]string{
	"HOME",
	"LANG",
	"LC_ALL",
	"LC_CTYPE",
	"PATH",
	"TMPDIR",
	"TZ",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_RUNTIME_DIR",
	"XDG_STATE_HOME",
}

// hcomSeatIdentityEnv is a security boundary between the launching pane and
// hcom's identity selection. Only process-runtime basics cross it; tool and
// identity signals are generated here so CLAUDE*/CODEX*/ambient HCOM_* values
// cannot select hcom's hook-install path or rewrite the bridge-owned identity.
func hcomSeatIdentityEnv(seat string) []string {
	env := make([]string, 0, len(hcomSeatIdentityEnvAllowlist)+2)
	for _, key := range hcomSeatIdentityEnvAllowlist {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return append(env, "HCOM_PROCESS_ID="+seat, "HCOM_TOOL=adhoc")
}

func (b *Binder) runHcomEnv(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, b.cfg.HcomBin, args...)
	if b.cfg.HcomDir != "" {
		env = replaceEnv(env, "HCOM_DIR", b.cfg.HcomDir)
	}
	cmd.Env = env
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("hcom %s failed: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}
func scrubEnv(env []string, names ...string) []string {
	drop := map[string]bool{}
	for _, n := range names {
		drop[n] = true
	}
	out := env[:0]
	for _, v := range env {
		k, _, _ := strings.Cut(v, "=")
		if !drop[k] {
			out = append(out, v)
		}
	}
	return out
}
func replaceEnv(env []string, k, v string) []string {
	env = scrubEnv(env, k)
	return append(env, k+"="+v)
}

func (b *Binder) send(req Request) (string, error) {
	if len(req.To) == 0 {
		return "", errors.New("send_message requires at least one recipient")
	}
	if err := b.refreshAndRecover(context.Background()); err != nil {
		return "", err
	}
	args := []string{"send"}
	for _, to := range req.To {
		if !strings.HasPrefix(to, "@") {
			to = "@" + to
		}
		args = append(args, to)
	}
	args = append(args, "--name", b.cfg.BusName)
	if req.Intent != "" {
		args = append(args, "--intent", req.Intent)
	}
	if req.Thread != "" {
		args = append(args, "--thread", req.Thread)
	}
	if req.ReplyTo != "" {
		args = append(args, "--reply-to", req.ReplyTo)
	}
	args = append(args, "--", req.Text)
	out, err := b.runHcom(context.Background(), false, args...)
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		out = "sent"
	}
	if err := b.journal.RecordOutbound(out); err != nil {
		return "", err
	}
	return out, nil
}
