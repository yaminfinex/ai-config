package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

type LockedUpdate struct {
	Projection *v2.Projection
	WasMinted  bool
	NodeID     string
}

type LockedUpdateFunc func(LockedUpdate) ([]v2.SessionRecord, error)

type WriteStatus string

const (
	WriteApplied WriteStatus = "applied"
	WriteNoop    WriteStatus = "noop"
	WriteRefused WriteStatus = "refused"
)

type WriteOutcome struct {
	Status WriteStatus
	Row    []byte
	Reason string
	cause  error
}

func (o WriteOutcome) Err() error {
	if o.Status != WriteRefused {
		return nil
	}
	if o.Reason == "" {
		return errors.New("registry write refused without a reason")
	}
	if o.cause != nil {
		return o.cause
	}
	return errors.New(o.Reason)
}

// UpdateLocked is the observer's sole cache write path. It serializes the
// whole batch under flock, validates cache row shape and node identity, then
// appends and fsyncs every accepted row before releasing the lock.
func UpdateLocked(path string, fn LockedUpdateFunc) ([]WriteOutcome, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return nil, fmt.Errorf("registry lock unavailable for %s: refusing to write unlocked: %w", path, err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	proj, err := v2.Load(f, v2.LoadOptions{})
	if err != nil {
		return nil, err
	}
	wasMinted := len(proj.Nodes()) > 0
	nodeID, _, proj, err := ensureLockedNode(path, f, proj)
	if err != nil {
		return nil, err
	}
	rotated, proj, err := rotateIfNeededLocked(path, f, proj)
	if err != nil {
		return nil, err
	}

	rows, err := fn(LockedUpdate{Projection: proj, WasMinted: wasMinted, NodeID: nodeID})
	if err != nil {
		return nil, err
	}
	outcomes := make([]WriteOutcome, 0, len(rows))
	for i, row := range rows {
		if current := v2ByGUID(proj, row.GUID); current != nil && !sessionHasRegisteredNode(proj, *current) {
			reason := fmt.Errorf("registry refused to mutate guid %s: latest row is attributed to unknown node %s (no node_registered row)", current.GUID, current.Node)
			return refusedBatch(outcomes, len(rows), i, reason), nil
		}
		normalized, err := normalizeObserverCacheAppend(v2ByGUID(proj, row.GUID), row)
		if err != nil {
			return refusedBatch(outcomes, len(rows), i, err), nil
		}
		normalized, err = stampSessionNode(normalized, nodeID)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return nil, err
		}
		proj, err = projectionWithAppended(proj, encoded)
		if err != nil {
			return nil, err
		}
		outcomes = append(outcomes, WriteOutcome{Status: WriteApplied, Row: encoded})
	}

	if len(outcomes) == 0 {
		if rotated {
			return outcomes, f.Sync()
		}
		return outcomes, nil
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return nil, err
	}
	for _, outcome := range outcomes {
		if _, err := f.Write(append(bytes.TrimRight(outcome.Row, "\n"), '\n')); err != nil {
			return nil, err
		}
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	return outcomes, nil
}

func normalizeObserverCacheAppend(current *v2.SessionRecord, row v2.SessionRecord) (v2.SessionRecord, error) {
	if row.GUID == "" {
		return row, fmt.Errorf("session row missing guid")
	}
	if current == nil {
		return row, fmt.Errorf("observer cache target %s not found", row.GUID)
	}
	if row.RecordedAt == "" {
		return row, fmt.Errorf("observer cache event %q requires recorded_at", row.Event)
	}
	switch row.Event {
	case "observed", "observed_dead", "observation_archived":
	default:
		return row, fmt.Errorf("unsupported registry event %q: the observer writes cache observations only", row.Event)
	}
	if row.Cache == nil || row.Cache.ObservedAt == "" || row.Cache.Liveness == "" {
		return row, fmt.Errorf("observer cache event %q requires liveness and observed_at", row.Event)
	}
	row.Kind = v2.KindSession
	row.Node = current.Node
	switch row.Event {
	case "observed":
		if row.State != v2.StateSeated || row.Seat == nil {
			return row, fmt.Errorf("observed cache stamp requires a seated row")
		}
	case "observed_dead":
		if row.State != v2.StateUnseated || row.Seat != nil {
			return row, fmt.Errorf("observed_dead cache stamp requires an unseated row")
		}
	case "observation_archived":
		if row.State != v2.StateRetired || row.Seat != nil {
			return row, fmt.Errorf("observation_archived cache stamp requires a retired row")
		}
	}
	return row, nil
}

func refusedBatch(prior []WriteOutcome, candidateCount, refusedAt int, cause error) []WriteOutcome {
	outcomes := make([]WriteOutcome, candidateCount)
	copy(outcomes, prior)
	for i := range outcomes {
		var reason string
		switch {
		case outcomes[i].Status == WriteApplied:
			reason = fmt.Sprintf("batch refused atomically because candidate %d was refused: %v", refusedAt+1, cause)
		case i == refusedAt:
			reason = cause.Error()
		case i > refusedAt:
			reason = fmt.Sprintf("candidate was not evaluated because candidate %d refused the atomic batch: %v", refusedAt+1, cause)
		default:
			continue
		}
		outcomeCause := error(errors.New(reason))
		if i == refusedAt {
			outcomeCause = cause
		}
		outcomes[i] = WriteOutcome{Status: WriteRefused, Reason: reason, cause: outcomeCause}
	}
	return outcomes
}

func ensureLockedNode(path string, f *os.File, proj *v2.Projection) (string, []byte, *v2.Projection, error) {
	markerPath := nodeMarkerPath(path)
	marker, markerPresent, err := readNodeMarker(markerPath)
	if err != nil {
		return "", nil, proj, err
	}
	nodes := proj.Nodes()
	if markerPresent && hasNode(nodes, marker) {
		return marker, nil, proj, nil
	}
	if !markerPresent && len(nodes) == 0 {
		return mintLockedNode(path, f, proj)
	}
	return "", nil, proj, nodeGateError(marker, markerPresent, nodes)
}

func mintLockedNode(path string, f *os.File, proj *v2.Projection) (string, []byte, *v2.Projection, error) {
	nodeID, err := newGUID()
	if err != nil {
		return "", nil, proj, err
	}
	row := v2.NodeRecord{Kind: v2.KindNode, Event: "node_registered", NodeID: nodeID, User: os.Getenv("USER"), Hostname: hostname(), RecordedAt: time.Now().UTC().Format(time.RFC3339)}
	encoded, err := json.Marshal(row)
	if err != nil {
		return "", nil, proj, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return "", nil, proj, err
	}
	if _, err := f.Write(append(encoded, '\n')); err != nil {
		return "", nil, proj, err
	}
	if err := writeNodeMarker(nodeMarkerPath(path), nodeID); err != nil {
		return "", nil, proj, err
	}
	if err := f.Sync(); err != nil {
		return "", nil, proj, err
	}
	next, err := projectionWithAppended(proj, encoded)
	return nodeID, encoded, next, err
}

func stampSessionNode(row v2.SessionRecord, nodeID string) (v2.SessionRecord, error) {
	if nodeID == "" {
		return row, fmt.Errorf("registry node gate failed: empty local node id")
	}
	row.Node = nodeID
	if row.Seat != nil {
		row.Seat.Node = nodeID
	}
	return row, nil
}

func lockFile(f *os.File) error {
	if os.Getenv("HERDER_TEST_FLOCK_REFUSE") == "1" {
		return syscall.ENOLCK
	}
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func projectionWithAppended(prev *v2.Projection, row []byte) (*v2.Projection, error) {
	var buf bytes.Buffer
	appendRaw := func(raw []byte) {
		if len(bytes.TrimSpace(raw)) == 0 {
			return
		}
		buf.Write(bytes.TrimRight(raw, "\n"))
		buf.WriteByte('\n')
	}
	for _, rec := range prev.Sessions() {
		appendRaw(rec.Raw)
	}
	for _, rec := range prev.Nodes() {
		appendRaw(rec.Raw)
	}
	for _, rec := range prev.Namespaces() {
		appendRaw(rec.Raw)
	}
	for _, rec := range prev.Epochs() {
		appendRaw(rec.Raw)
	}
	buf.Write(bytes.TrimRight(row, "\n"))
	buf.WriteByte('\n')
	return v2.Load(&buf, v2.LoadOptions{})
}

func v2ByGUID(proj *v2.Projection, guid string) *v2.SessionRecord {
	for _, rec := range proj.Sessions() {
		if rec.GUID == guid {
			copy := rec
			return &copy
		}
	}
	return nil
}

func readNodeMarker(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	id := strings.TrimSpace(string(data))
	if id == "" || !isNodeIDShape(id) {
		return "", false, fmt.Errorf("registry node gate refused: malformed node marker %s contains %q; restore the marker from the registry's node row before retrying", path, id)
	}
	return id, true, nil
}

func writeNodeMarker(path, nodeID string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(nodeID + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func hasNode(nodes []v2.NodeRecord, nodeID string) bool {
	for _, node := range nodes {
		if node.NodeID == nodeID {
			return true
		}
	}
	return false
}

func sessionHasRegisteredNode(proj *v2.Projection, rec v2.SessionRecord) bool {
	return rec.Node == "" || hasNode(proj.Nodes(), rec.Node)
}

func nodeGateError(marker string, markerPresent bool, nodes []v2.NodeRecord) error {
	state := "marker and registry node state are inconsistent"
	switch {
	case markerPresent && len(nodes) == 0:
		state = fmt.Sprintf("marker contains %s but registry has no node_registered row", marker)
	case !markerPresent && len(nodes) > 0:
		state = fmt.Sprintf("registry has node_registered row %s but marker is absent", nodes[0].NodeID)
	case markerPresent && len(nodes) > 0:
		state = fmt.Sprintf("marker contains %s but registry node rows are %s", marker, nodeIDs(nodes))
	}
	return fmt.Errorf("registry node gate refused: %s; restore a matching marker and node row before retrying", state)
}

func nodeIDs(nodes []v2.NodeRecord) string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.NodeID)
	}
	return strings.Join(out, ",")
}

func isNodeIDShape(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, r := range id {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

func hostname() string {
	host, _ := os.Hostname()
	return host
}
