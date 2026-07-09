package ship

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"sesh/internal/wire"
)

// fakeStore implements the frozen wire contract (docs/specs/sesh-wire.md)
// faithfully enough to characterize every required shipper reaction:
// append/ACK, identical-replay, offset gap, fingerprint routing, the
// byte-conflict → generation_opened → poisoned handshake, and recovery GET.
// It exists because U3 (the real store) builds in a parallel lane; U5 gates
// the two against each other for real.
type fakeStore struct {
	mu    sync.Mutex
	files map[string]*fakeFile // key: tool/session/file_uuid

	// unavailable makes every PUT answer 503.
	unavailable bool
	// nonConformingFingerprintInform makes the first fingerprint-routed PUT
	// answer 409 fingerprint_conflict carrying the matched generation and its
	// high_water instead of routing silently. A conforming store never does
	// this (Amendment 2: fingerprint_conflict only ever opens a new empty
	// generation, high_water 0); the knob exists solely so tests can prove the
	// shipper's verbatim error-rewind tolerates a non-conforming store (U4
	// review finding #1).
	nonConformingFingerprintInform bool
	// putLog records every PUT offset per identity key, for assertions like
	// "no re-ship from zero after a move".
	putLog map[string][]int64
	// ownerLog records the X-Sesh-Session-Owner header of every PUT per
	// identity key ("" when absent) — the facts observation surface (U9).
	ownerLog map[string][]string
}

type fakeFile struct {
	generations []*fakeGen
	poisoned    bool
	// conflictOpenedFor notes that a conflict-driven generation was already
	// opened for a fingerprint; recurrence poisons.
	conflictOpenedFor map[string]bool
	// informedFP tracks which fingerprints already got the one non-conforming
	// inform; used only under nonConformingFingerprintInform.
	informedFP map[string]bool
}

type fakeGen struct {
	fingerprint string // "" = null (below window)
	data        []byte
	// conflictPending is set after a divergent PUT against this generation
	// and cleared by any successful PUT (wire doc: "no intervening
	// successful PUT").
	conflictPending bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{files: map[string]*fakeFile{}, putLog: map[string][]int64{}, ownerLog: map[string][]string{}}
}

func (fs *fakeStore) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(fs.handle))
}

func (fs *fakeStore) key(tool, sid, fuuid string) string { return tool + "/" + sid + "/" + fuuid }

// mirrorBytes returns the active generation's mirrored bytes for an identity.
func (fs *fakeStore) mirrorBytes(tool, sid, fuuid string) []byte {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f := fs.files[fs.key(tool, sid, fuuid)]
	if f == nil || len(f.generations) == 0 {
		return nil
	}
	return append([]byte(nil), f.generations[len(f.generations)-1].data...)
}

func (fs *fakeStore) generationCount(tool, sid, fuuid string) int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f := fs.files[fs.key(tool, sid, fuuid)]
	if f == nil {
		return 0
	}
	return len(f.generations)
}

func (fs *fakeStore) generationBytes(tool, sid, fuuid string, gen int) []byte {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f := fs.files[fs.key(tool, sid, fuuid)]
	if f == nil || gen >= len(f.generations) {
		return nil
	}
	return append([]byte(nil), f.generations[gen].data...)
}

func (fs *fakeStore) puts(tool, sid, fuuid string) []int64 {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]int64(nil), fs.putLog[fs.key(tool, sid, fuuid)]...)
}

// owners returns the session-owner header value of every PUT, in order.
func (fs *fakeStore) owners(tool, sid, fuuid string) []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]string(nil), fs.ownerLog[fs.key(tool, sid, fuuid)]...)
}

// seed pre-loads a generation, as if shipped earlier (for recovery tests).
func (fs *fakeStore) seed(tool, sid, fuuid, fingerprint string, data []byte) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.files[fs.key(tool, sid, fuuid)] = &fakeFile{
		generations:       []*fakeGen{{fingerprint: fingerprint, data: append([]byte(nil), data...)}},
		conflictOpenedFor: map[string]bool{},
		informedFP:        map[string]bool{},
	}
}

// seedExtra appends one more (newer, active) generation to a seeded file.
func (fs *fakeStore) seedExtra(tool, sid, fuuid, fingerprint string, data []byte) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f := fs.files[fs.key(tool, sid, fuuid)]
	f.generations = append(f.generations, &fakeGen{fingerprint: fingerprint, data: append([]byte(nil), data...)})
}

func (fs *fakeStore) setPoisoned(tool, sid, fuuid string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if f := fs.files[fs.key(tool, sid, fuuid)]; f != nil {
		f.poisoned = true
	}
}

func (fs *fakeStore) handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, wire.APIRoot+"/files/"), "/")
	writeErr := func(status int, code wire.ErrorCode, gen int, highWater int64) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(wire.ErrorResponse{
			WireVersion: wire.Version, Code: code, Message: "fake store: " + string(code),
			Generation: gen, HighWater: highWater,
		})
	}
	if len(parts) < 3 {
		writeErr(400, wire.ErrMalformedRequest, 0, 0)
		return
	}
	tool, sid, fuuid := parts[0], parts[1], parts[2]
	if tool != string(wire.ToolClaude) && tool != string(wire.ToolCodex) {
		writeErr(400, wire.ErrUnknownTool, 0, 0)
		return
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if r.Method == http.MethodGet {
		fs.handleRecovery(w, tool, sid, fuuid, writeErr)
		return
	}
	if r.Method != http.MethodPut || len(parts) != 4 || parts[3] != "bytes" {
		writeErr(400, wire.ErrMalformedRequest, 0, 0)
		return
	}
	if fs.unavailable {
		writeErr(503, wire.ErrStoreUnavailable, 0, 0)
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || offset < 0 || r.Header.Get(wire.HeaderWireVersion) != strconv.Itoa(wire.Version) {
		writeErr(400, wire.ErrMalformedRequest, 0, 0)
		return
	}
	// Required headers per the frozen wire doc (U4 review finding #4: the
	// fake must be as strict as a conforming store).
	if r.Header.Get("Content-Type") != wire.ContentTypeBytes ||
		r.Header.Get(wire.HeaderHostname) == "" ||
		r.Header.Get(wire.HeaderOSUser) == "" {
		writeErr(400, wire.ErrMalformedRequest, 0, 0)
		return
	}
	reqFP := r.Header.Get(wire.HeaderFingerprint)
	if reqFP != "" && r.Header.Get(wire.HeaderFingerprintAlgorithm) != wire.FingerprintAlgorithm {
		writeErr(400, wire.ErrMalformedRequest, 0, 0)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, wire.MaxPUTBody+1))
	if err != nil {
		writeErr(500, wire.ErrMirrorWriteFailed, 0, 0)
		return
	}
	if len(body) > wire.MaxPUTBody {
		writeErr(413, wire.ErrBodyTooLarge, 0, 0)
		return
	}
	key := fs.key(tool, sid, fuuid)
	fs.putLog[key] = append(fs.putLog[key], offset)
	fs.ownerLog[key] = append(fs.ownerLog[key], r.Header.Get(wire.HeaderSessionOwner))

	f := fs.files[key]
	if f == nil {
		f = &fakeFile{
			generations:       []*fakeGen{{fingerprint: reqFP}},
			conflictOpenedFor: map[string]bool{},
			informedFP:        map[string]bool{},
		}
		fs.files[key] = f
	}
	if f.informedFP == nil {
		f.informedFP = map[string]bool{}
	}
	if f.poisoned {
		writeErr(423, wire.ErrPoisonedFile, len(f.generations)-1, 0)
		return
	}

	// Fingerprint routing before offset routing. Absence is never a
	// mismatch; a claim against a null-recorded generation records it.
	genIdx := len(f.generations) - 1
	gen := f.generations[genIdx]
	if reqFP != "" && gen.fingerprint == "" {
		gen.fingerprint = reqFP
	}
	if reqFP != "" && reqFP != gen.fingerprint {
		matched := -1
		for i, g := range f.generations {
			if g.fingerprint == reqFP {
				matched = i // ascending: last match = highest
			}
		}
		if matched < 0 {
			// Amendment 2 (W1): a new fingerprint opens a new, empty
			// generation — the ONLY case that returns fingerprint_conflict,
			// always with high_water 0.
			f.generations = append(f.generations, &fakeGen{fingerprint: reqFP})
			writeErr(409, wire.ErrFingerprintConflict, len(f.generations)-1, 0)
			return
		}
		if fs.nonConformingFingerprintInform && !f.informedFP[reqFP] {
			// Deliberately non-conforming: announce the selected generation
			// and ITS high-water (which may exceed the shipper's local size —
			// the review finding #1 scenario) instead of routing silently.
			f.informedFP[reqFP] = true
			writeErr(409, wire.ErrFingerprintConflict, matched, int64(len(f.generations[matched].data)))
			return
		}
		// Amendment 2 (W1): silent route-through to the highest-numbered
		// matching generation; the response envelope carries its number.
		genIdx, gen = matched, f.generations[matched]
	}

	highWater := int64(len(gen.data))
	switch {
	case offset == highWater:
		gen.data = append(gen.data, body...)
		gen.conflictPending = false
		fs.writeAck(w, tool, sid, fuuid, genIdx, int64(len(gen.data)), gen.fingerprint)
	case offset < highWater:
		end := offset + int64(len(body))
		if end > highWater {
			end = highWater
		}
		if bytes.Equal(gen.data[offset:end], body[:end-offset]) {
			// Identical replay: silent success. When the range extends past
			// high-water with a matching overlap, the excess is appended
			// (compare-overlap-and-append-excess, Amendment 1).
			if excess := body[end-offset:]; len(excess) > 0 {
				gen.data = append(gen.data, excess...)
			}
			gen.conflictPending = false
			fs.writeAck(w, tool, sid, fuuid, genIdx, int64(len(gen.data)), gen.fingerprint)
			return
		}
		// Divergence: first sight = byte_conflict, no state change; second
		// consecutive = open a generation, or poison on recurrence.
		if !gen.conflictPending {
			gen.conflictPending = true
			writeErr(409, wire.ErrByteConflict, genIdx, highWater)
			return
		}
		fpKey := gen.fingerprint
		if f.conflictOpenedFor[fpKey] {
			f.poisoned = true
			writeErr(423, wire.ErrPoisonedFile, genIdx, highWater)
			return
		}
		f.conflictOpenedFor[fpKey] = true
		gen.conflictPending = false
		f.generations = append(f.generations, &fakeGen{fingerprint: gen.fingerprint})
		writeErr(409, wire.ErrGenerationOpened, len(f.generations)-1, 0)
	default: // offset > highWater
		writeErr(422, wire.ErrOffsetGap, genIdx, highWater)
	}
}

func (fs *fakeStore) writeAck(w http.ResponseWriter, tool, sid, fuuid string, gen int, hw int64, fp string) {
	var fpp *string
	if fp != "" {
		fpp = &fp
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(wire.Ack{
		WireVersion: wire.Version, Status: wire.StatusAck,
		Tool: wire.Tool(tool), SessionID: sid, FileUUID: fuuid,
		Generation: gen, HighWater: hw,
		FingerprintAlgorithm: wire.FingerprintAlgorithm, Fingerprint: fpp,
	})
}

func (fs *fakeStore) handleRecovery(w http.ResponseWriter, tool, sid, fuuid string, writeErr func(int, wire.ErrorCode, int, int64)) {
	f := fs.files[fs.key(tool, sid, fuuid)]
	if f == nil {
		writeErr(404, wire.ErrNotFound, 0, 0)
		return
	}
	resp := wire.RecoveryResponse{
		WireVersion: wire.Version, Tool: wire.Tool(tool), SessionID: sid, FileUUID: fuuid,
		FingerprintAlgorithm: wire.FingerprintAlgorithm, FingerprintWindowBytes: wire.FingerprintWindowBytes,
	}
	for i, g := range f.generations {
		var fpp *string
		if g.fingerprint != "" {
			fp := g.fingerprint
			fpp = &fp
		}
		resp.Generations = append(resp.Generations, wire.GenerationState{
			Generation: i, Fingerprint: fpp, HighWater: int64(len(g.data)),
			Poisoned: f.poisoned, LastPutAt: time.Now().UTC(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// sanity-check helper: fail fast if the fake violates its own invariants.
func (fs *fakeStore) String() string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var b strings.Builder
	for k, f := range fs.files {
		fmt.Fprintf(&b, "%s: %d gens poisoned=%v\n", k, len(f.generations), f.poisoned)
	}
	return b.String()
}
