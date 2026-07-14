package ship

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	"sesh/internal/wire"
)

// Shipper drives discovery, cursors, and tailing for one OS user. RunOnce is
// one authoritative rescan pass (what the 60s ticker and the tests run);
// Run is the daemon loop layering fsnotify hints over the ticker.
type Shipper struct {
	Registry *Registry
	Client   *Client
	Roots    Roots

	// MaxBody caps one PUT body; defaults to wire.MaxPUTBody.
	MaxBody int
	// Rescan is the authoritative rescan interval; defaults to
	// wire.RescanInterval.
	Rescan time.Duration
	// Backoff returns the hold delay before retry attempt n (1-based);
	// defaults to jittered exponential capped at 30s.
	Backoff func(attempt int) time.Duration
	Logger  *slog.Logger
	// Correlate observes SESSION_OWNER for the discovered files, one call
	// per authoritative pass (spec §4.2): identity key → owner. Nil where no
	// correlation exists (darwin ships facts-only). An identity absent from
	// the result is honest absence and never retracts a recorded owner (I8).
	Correlate func([]Discovered) map[string]string

	// hintInterval is the minimum interval between authoritative passes
	// admitted by filesystem hints. Tests shorten it; zero uses the default.
	hintInterval time.Duration

	// fileConcurrency bounds the parallel per-file workers within one
	// authoritative pass. Zero uses defaultFileConcurrency; tests set 1 to
	// characterize the serialized baseline. Deliberately not a config knob:
	// the default is chosen against the shared store, not a node property.
	fileConcurrency int

	// passMu enforces the pass-level half of the ordering invariant inside
	// Shipper itself: RunOnce is exclusive, so "passes never overlap" holds
	// for any caller, not as an assumption about the sequential daemon loop
	// in Run. A re-entrant RunOnce would otherwise hand one identity to two
	// workers and race on NeedsRecovery. A mutex rather than a refusal: a
	// concurrent caller gets the correct serialized pass instead of a new
	// error path to handle.
	passMu sync.Mutex

	// heldMu guards held: parallel workers within a pass record and consult
	// non-retryable refusals concurrently.
	heldMu sync.Mutex
	// held parks identities that hit a non-retryable error
	// (malformed_request, unknown_tool) until the process restarts: no retry
	// loop, surfaced loudly instead.
	held map[string]string
}

const defaultHintInterval = 2 * time.Second

// defaultFileConcurrency bounds simultaneous per-file operations (recovery
// GETs, PUT streams) in one pass. The first pass over a fresh corpus is
// RTT-serialized per file without it (3-5k files at WAN RTT = 10+ minutes in
// round trips alone), so single-file latency wants a high bound — but every
// PUT lands in one write transaction on the store's single write connection
// (docs/design/2026-07-13-sesh-store-read-write-split.md: an append at corpus
// scale can hold that connection ~0.5s), so a large bound just moves the
// queue server-side and starves the other shippers sharing the store. 8 keeps
// the RTT component of a 3k-file first pass under a couple of minutes while
// capping this node's standing demand on the shared write path.
const defaultFileConcurrency = 8

func (s *Shipper) concurrency() int {
	if s.fileConcurrency > 0 {
		return s.fileConcurrency
	}
	return defaultFileConcurrency
}

func (s *Shipper) minHintInterval() time.Duration {
	if s.hintInterval > 0 {
		return s.hintInterval
	}
	return defaultHintInterval
}

// errHold marks a hold-position condition (store unreachable/unavailable,
// out-of-grant, mirror write failure): the cursor does not advance, nothing
// is queued locally (the source file is the only buffer), and the daemon
// retries with backoff.
var errHold = errors.New("hold position")

func (s *Shipper) maxBody() int {
	if s.MaxBody > 0 {
		return s.MaxBody
	}
	return wire.MaxPUTBody
}

func (s *Shipper) rescan() time.Duration {
	if s.Rescan > 0 {
		return s.Rescan
	}
	return wire.RescanInterval
}

func (s *Shipper) backoff(attempt int) time.Duration {
	if s.Backoff != nil {
		return s.Backoff(attempt)
	}
	d := time.Second << min(attempt, 5) // 2s..32s
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d/2 + rand.N(d/2) // jitter: [d/2, d)
}

func (s *Shipper) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// RunOnce performs one authoritative pass: discover, rebuild cursors via
// recovery GETs when the registry was lost, GC cursors whose files are gone,
// then ship every discovered file to quiescence. Hold-class conditions come
// back errHold-wrapped; the pass still visits every other file first.
func (s *Shipper) RunOnce(ctx context.Context) (runErr error) {
	s.passMu.Lock()
	defer s.passMu.Unlock()
	s.Registry.beginBatch()
	defer func() {
		if err := s.Registry.endBatch(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("cursor registry batch flush: %w", err))
		}
	}()

	discovered, err := Discover(s.Roots)
	if err != nil {
		return err
	}
	// Ordering invariant under the bounded-parallel pass: at most one
	// in-flight operation per file identity, ever. Across identities the wire
	// is order-independent (each PUT/recovery GET carries only that
	// identity's offset and fingerprint; the store keys all state per
	// identity), but WITHIN an identity the append protocol assumes strictly
	// sequential PUTs against a known high-water. Both are guaranteed by
	// construction: passes never overlap (RunOnce serializes on passMu),
	// and within a pass each identity is handed to exactly one worker — which
	// requires deduping here, because Discover can return the same identity
	// at two paths (a copied project directory). The serial code tolerated
	// duplicates by shipping them back to back; parallel workers must not.
	discovered = dedupeByIdentity(discovered)
	present := make(map[string]bool, len(discovered))
	for _, d := range discovered {
		present[d.Identity.Key()] = true
	}

	if s.Registry.NeedsRecovery {
		// One recovery GET per discovered identity is a long, otherwise
		// silent stretch on a fresh registry over a large corpus; say so
		// before it starts instead of looking wedged.
		s.logger().Info("cursor registry missing or unreadable; recovering positions from store", "files", len(discovered))
		if err := s.forEachFile(ctx, discovered, func(d Discovered) error {
			if err := s.recoverCursor(ctx, d); err != nil {
				return fmt.Errorf("cursor recovery for %s: %w", d.Identity.Key(), err)
			}
			return nil
		}); err != nil {
			// NeedsRecovery stays set: recovered identities have cursors, so
			// the retried pass resumes behind them instead of restarting.
			return err
		}
		// All discovered identities recovered; the registry is authoritative
		// again.
		s.Registry.NeedsRecovery = false
	}

	// Deletion is not truncation: a tracked identity with no file left GCs
	// its cursor; the mirror retains (I6, I7).
	for _, c := range s.Registry.All() {
		if !present[c.Identity().Key()] {
			if err := s.Registry.Delete(c.Identity()); err != nil {
				return err
			}
			s.logger().Info("cursor GC after deletion", "identity", c.Identity().Key())
		}
	}

	// SESSION_OWNER correlation (spec §4.2), one call per pass, before the
	// files ship so the observation rides this pass's PUTs. A returned owner
	// is recorded durably in the registry; an identity absent from the
	// result changes nothing — an observation is never retracted (I8).
	if err := s.recordOwnerObservations(discovered); err != nil {
		return err
	}

	return s.forEachFile(ctx, discovered, func(d Discovered) error {
		if err := s.shipFile(ctx, d); err != nil {
			return fmt.Errorf("%s: %w", d.Identity.Key(), err)
		}
		return nil
	})
}

// forEachFile runs fn once per discovered file on at most concurrency()
// workers and joins every error: one file's failure never aborts the pass
// for the others (the joined result still errors.Is-matches errHold when any
// hold occurred). Callers must have deduped by identity — one worker owns an
// identity for the whole pass.
func (s *Shipper) forEachFile(ctx context.Context, files []Discovered, fn func(Discovered) error) error {
	sem := make(chan struct{}, s.concurrency())
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for _, d := range files {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(d); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// dedupeByIdentity keeps the first discovery of each identity, preserving
// walk order. See the ordering invariant in RunOnce.
func dedupeByIdentity(files []Discovered) []Discovered {
	seen := make(map[string]bool, len(files))
	out := files[:0]
	for _, d := range files {
		if seen[d.Identity.Key()] {
			continue
		}
		seen[d.Identity.Key()] = true
		out = append(out, d)
	}
	return out
}

func (s *Shipper) recordOwnerObservations(discovered []Discovered) error {
	if s.Correlate == nil {
		return nil
	}
	owners := s.Correlate(discovered)
	recorded := false
	for _, d := range discovered {
		owner := owners[d.Identity.Key()]
		if owner == "" {
			continue
		}
		cur, ok := s.Registry.Get(d.Identity)
		if !ok {
			cur = Cursor{Tool: d.Identity.Tool, SessionID: d.Identity.SessionID, FileUUID: d.Identity.FileUUID, Path: d.Path}
		}
		if cur.SessionOwner != owner {
			cur.SessionOwner = owner
			if err := s.Registry.Put(cur); err != nil {
				return err
			}
			recorded = true
			s.logger().Info("SESSION_OWNER observed", "identity", d.Identity.Key(), "owner", owner)
		}
	}
	if recorded {
		// Process correlation is ephemeral: unlike ACKed bytes, a crash
		// cannot reconstruct this observation by replaying the source file.
		return s.Registry.flush()
	}
	return nil
}

// recoverCursor rebuilds one identity's cursor from a recovery GET, per the
// wire doc's required reactions to a 200 recovery response.
func (s *Shipper) recoverCursor(ctx context.Context, d Discovered) error {
	if _, ok := s.Registry.Get(d.Identity); ok {
		return nil // already recovered in an earlier (partial) pass
	}
	rec, werr, err := s.Client.Recover(ctx, d.Identity)
	if err != nil {
		return fmt.Errorf("%w: %v", errHold, err)
	}
	if werr != nil {
		switch werr.Code {
		case wire.ErrNotFound:
			// No mirror state: the normal new-file path from 0. The zero
			// cursor is recorded so an interrupted recovery pass resumes
			// behind the identities the store already answered instead of
			// re-querying all of them — each is a full store round trip.
			return s.Registry.Put(Cursor{Tool: d.Identity.Tool, SessionID: d.Identity.SessionID, FileUUID: d.Identity.FileUUID, Path: d.Path})
		case wire.ErrOutOfGrant, wire.ErrStoreUnavailable, wire.ErrMirrorWriteFailed:
			return fmt.Errorf("%w: recovery GET %s", errHold, werr.Code)
		default:
			return fmt.Errorf("recovery GET refused: %s (%s)", werr.Code, werr.Message)
		}
	}

	cur := Cursor{Tool: d.Identity.Tool, SessionID: d.Identity.SessionID, FileUUID: d.Identity.FileUUID, Path: d.Path}
	localFP, ready, err := Fingerprint(d.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // vanished mid-pass; next rescan settles it
		}
		return err
	}
	// The generation the shipper would resume: the highest-numbered
	// fingerprint match, else (from 0) the active generation.
	var target *wire.GenerationState
	if ready {
		cur.Fingerprint = localFP
		for i := range rec.Generations {
			g := &rec.Generations[i]
			if g.Fingerprint != nil && *g.Fingerprint == localFP {
				target = g // ascending order: last match = highest
			}
		}
	}
	if target != nil {
		cur.Offset = target.HighWater
	} else if len(rec.Generations) > 0 {
		target = &rec.Generations[len(rec.Generations)-1] // active; ship from 0
	}
	if target != nil && target.Poisoned {
		cur.Poisoned = true
		s.logger().Warn("recovered identity is poisoned; parking", "identity", d.Identity.Key())
	}
	return s.Registry.Put(cur)
}

// shipFile runs one file to quiescence, implementing the file-identity state
// diagram literally: size regression before fingerprint comparison, cursor
// advance only on durable ACK, and the frozen error-catalog reactions.
func (s *Shipper) shipFile(ctx context.Context, d Discovered) error {
	if reason, ok := s.heldReason(d.Identity.Key()); ok {
		s.logger().Warn("file held (non-retryable store refusal); restart after remedy", "identity", d.Identity.Key(), "reason", reason)
		return nil
	}
	cur, ok := s.Registry.Get(d.Identity)
	if !ok {
		cur = Cursor{Tool: d.Identity.Tool, SessionID: d.Identity.SessionID, FileUUID: d.Identity.FileUUID, Path: d.Path}
	}
	if cur.Poisoned {
		return nil // frozen cursor; deletion GC still applies
	}
	if cur.Path != d.Path {
		// Identity survives churn: a move updates the advisory path only —
		// no reset, no re-ship (I6).
		cur.Path = d.Path
		if err := s.Registry.Put(cur); err != nil {
			return err
		}
	}

	maxBody := s.maxBody()
	conflictRetried := false
	for {
		st, err := os.Stat(d.Path)
		if os.IsNotExist(err) {
			return nil // deletion; GC on the next authoritative pass
		}
		if err != nil {
			return err
		}
		size := st.Size()

		// Size regression fires BEFORE fingerprint comparison (wire doc,
		// File Identity): truncation → reset to 0 and re-ship; the mirror
		// absorbs the replay.
		if size < cur.Offset {
			s.logger().Info("size regression: truncation reset", "identity", d.Identity.Key(), "size", size, "cursor", cur.Offset)
			cur.Offset = 0
			cur.Fingerprint = ""
			if err := s.Registry.Put(cur); err != nil {
				return err
			}
		}

		fp, ready, err := Fingerprint(d.Path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if ready {
			if cur.Fingerprint != "" && fp != cur.Fingerprint {
				// Same UUID, different content window: recreated file.
				// Reset; the store's fingerprint routing opens or selects
				// the right generation.
				s.logger().Info("fingerprint mismatch: recreated file, reset", "identity", d.Identity.Key())
				cur.Offset = 0
			}
			if cur.Fingerprint != fp {
				cur.Fingerprint = fp
				if err := s.Registry.Put(cur); err != nil {
					return err
				}
			}
		}

		if cur.Offset >= size {
			return nil // quiescent
		}

		n := size - cur.Offset
		if n > int64(maxBody) {
			n = int64(maxBody)
		}
		body, err := readRange(d.Path, cur.Offset, int(n))
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if len(body) == 0 {
			continue // raced a concurrent change; re-stat
		}

		ack, werr, err := s.Client.PutBytes(ctx, d.Identity, cur.Offset, body, cur.Fingerprint, cur.SessionOwner)
		if err != nil {
			// Transport failure = store_unavailable: hold position, backoff,
			// no local queue.
			return fmt.Errorf("%w: %v", errHold, err)
		}
		if ack != nil {
			// Amendment 1: on any 200, cursor := min(returned high_water,
			// most recently observed source size) — the clamp that makes
			// same-prefix truncation quiesce (S3) instead of looping on the
			// store's longer high-water. The clamp applies to 200s ONLY: a
			// 200 means every local byte in the range was compared or
			// appended, so quiescing at local EOF is safe. Error-path
			// rewinds carry no such comparison and are adopted verbatim.
			next := ack.HighWater
			if next > size {
				next = size
			}
			cur.Offset = next
			cur.LastAckAt = time.Now().UTC()
			if err := s.Registry.Put(cur); err != nil {
				return err
			}
			conflictRetried = false
			continue
		}

		switch werr.Code {
		case wire.ErrOffsetGap, wire.ErrFingerprintConflict, wire.ErrGenerationOpened:
			// All three carry the high-water to rewind to (gap: current;
			// fingerprint_conflict: the selected generation's;
			// generation_opened: 0 for the fresh generation). Adopted
			// VERBATIM, never clamped to local size: no byte comparison has
			// happened on an error path, so clamping here can falsely
			// quiesce a divergent recreated file at local EOF and silently
			// lose its history (U4 review finding #1). A high-water beyond
			// the local size triggers the size-regression reset on the next
			// iteration, whose re-PUT forces the comparison.
			cur.Offset = werr.HighWater
			if err := s.Registry.Put(cur); err != nil {
				return err
			}
			conflictRetried = false
			continue
		case wire.ErrByteConflict:
			// Re-check local identity (the loop re-runs size regression and
			// re-fingerprint), then retry the same PUT once; the second
			// divergence yields generation_opened or poisoned_file from a
			// conforming store.
			if conflictRetried {
				return fmt.Errorf("store repeated byte_conflict after the single retry for %s: non-conforming store, surfacing instead of looping", d.Identity.Key())
			}
			conflictRetried = true
			continue
		case wire.ErrPoisonedFile:
			cur.Poisoned = true
			if err := s.Registry.Put(cur); err != nil {
				return err
			}
			s.logger().Error("store poisoned file; cursor frozen, not retrying", "identity", d.Identity.Key())
			return nil
		case wire.ErrOutOfGrant, wire.ErrStoreUnavailable, wire.ErrMirrorWriteFailed:
			return fmt.Errorf("%w: %s (%s)", errHold, werr.Code, werr.Message)
		case wire.ErrBodyTooLarge:
			if maxBody <= 1 {
				return fmt.Errorf("store rejects even 1-byte bodies as too large for %s", d.Identity.Key())
			}
			maxBody /= 2
			continue
		default: // malformed_request, unknown_tool, anything unrecognized
			s.holdFile(d.Identity.Key(), string(werr.Code))
			s.logger().Error("non-retryable store refusal; holding file until restart",
				"identity", d.Identity.Key(), "code", werr.Code, "message", werr.Message)
			return nil
		}
	}
}

func (s *Shipper) heldReason(key string) (string, bool) {
	s.heldMu.Lock()
	defer s.heldMu.Unlock()
	reason, ok := s.held[key]
	return reason, ok
}

func (s *Shipper) holdFile(key, reason string) {
	s.heldMu.Lock()
	defer s.heldMu.Unlock()
	if s.held == nil {
		s.held = map[string]string{}
	}
	s.held[key] = reason
}

func readRange(path string, offset int64, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	read, err := f.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:read], nil
}
