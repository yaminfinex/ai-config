package ship

// Characterization tests for the U4 churn cases (plan U4 execution note:
// each fixture churn case is encoded as a failing test BEFORE the state
// machine exists). Every scenario tag [S-n] is spec §6; fixture bytes are
// real captured transcripts from tests/fixtures (never synthesized).

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sesh/internal/wire"
)

const (
	uuidNormal  = "45308169-72e6-4cbe-a05c-2a0025db055e"
	uuidResumeA = "2c387aef-72ac-46bc-8ea5-e3b68690a937"
	uuidResumeB = "e1be75ad-151b-47fa-9d69-46de1c117843"
	uuidCodex   = "019f01cf-3d22-7ea0-923e-e463b90ea31e"
	uuidFresh   = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

type harness struct {
	t        *testing.T
	store    *fakeStore
	srv      *httptest.Server
	roots    Roots
	stateDir string
	shipper  *Shipper
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, store: newFakeStore()}
	h.srv = h.store.server()
	t.Cleanup(h.srv.Close)
	base := t.TempDir()
	h.roots = Roots{
		Claude: filepath.Join(base, "claude-projects"),
		Codex:  filepath.Join(base, "codex-sessions"),
	}
	h.stateDir = filepath.Join(base, "state")
	h.openShipper()
	return h
}

func (h *harness) openShipper() {
	h.t.Helper()
	reg, err := OpenRegistry(h.stateDir)
	if err != nil {
		h.t.Fatalf("open registry: %v", err)
	}
	h.t.Cleanup(reg.Close)
	h.shipper = &Shipper{
		Registry: reg,
		Client:   &Client{BaseURL: h.srv.URL, Hostname: "testhost", OSUser: "testuser"},
		Roots:    h.roots,
		Backoff:  func(int) time.Duration { return 0 },
	}
}

// restart simulates a process restart: release the flock, reopen the
// registry from disk, fresh Shipper (in-memory state lost).
func (h *harness) restart() {
	h.t.Helper()
	h.shipper.Registry.Close()
	h.openShipper()
}

func (h *harness) writeClaude(project, uuid string, data []byte) string {
	h.t.Helper()
	dir := filepath.Join(h.roots.Claude, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	p := filepath.Join(dir, uuid+".jsonl")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *harness) writeCodex(sub, uuid string, data []byte) string {
	h.t.Helper()
	dir := filepath.Join(h.roots.Codex, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	p := filepath.Join(dir, "rollout-2026-06-26T02-43-06-"+uuid+".jsonl")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *harness) runOnce() {
	h.t.Helper()
	if err := h.shipper.RunOnce(context.Background()); err != nil {
		h.t.Fatalf("RunOnce: %v", err)
	}
}

func (h *harness) assertMirror(tool, uuid string, want []byte) {
	h.t.Helper()
	got := h.store.mirrorBytes(tool, uuid, uuid)
	if string(got) != string(want) {
		h.t.Fatalf("mirror for %s/%s: got %d bytes, want %d (byte-compare failed)", tool, uuid, len(got), len(want))
	}
}

func (h *harness) cursor(tool wire.Tool, uuid string) (Cursor, bool) {
	return h.shipper.Registry.Get(Identity{Tool: tool, SessionID: uuid, FileUUID: uuid})
}

// --- S1: cold-start backfill (AC #1) ---------------------------------------

func TestColdStartBackfillShipsFixtureTree(t *testing.T) {
	h := newHarness(t)
	normal := fixture(t, "claude-normal.jsonl")
	resumeA := fixture(t, "claude-resume-original.jsonl")
	resumeB := fixture(t, "claude-resume-new-file.jsonl")
	codex := fixture(t, "codex-rollout-meta.jsonl")

	h.writeClaude("-home-user-proj-a", uuidNormal, normal)
	h.writeClaude("-home-user-proj-b", uuidResumeA, resumeA)
	h.writeClaude("-home-user-proj-b", uuidResumeB, resumeB)
	h.writeCodex("2026/06/26", uuidCodex, codex)
	// Noise that must be ignored by the discovery globs.
	h.writeClaude("-home-user-proj-a", uuidFresh, []byte("x"))
	os.Remove(filepath.Join(h.roots.Claude, "-home-user-proj-a", uuidFresh+".jsonl"))
	os.WriteFile(filepath.Join(h.roots.Claude, "-home-user-proj-a", "notes.txt"), []byte("ignore me"), 0o644)

	h.runOnce()

	h.assertMirror("claude", uuidNormal, normal)
	h.assertMirror("claude", uuidResumeA, resumeA)
	h.assertMirror("claude", uuidResumeB, resumeB)
	h.assertMirror("codex", uuidCodex, codex)

	c, ok := h.cursor(wire.ToolClaude, uuidNormal)
	if !ok || c.Offset != int64(len(normal)) {
		t.Fatalf("cursor after backfill: %+v ok=%v want offset %d", c, ok, len(normal))
	}
	if c.Fingerprint == "" {
		t.Fatal("file above window must have a recorded fingerprint")
	}
}

// --- S3: truncation below cursor → single reset + re-ship, no loop (AC #2) -

func TestTruncationBelowCursorResetsOnceNoLoop(t *testing.T) {
	h := newHarness(t)
	full := fixture(t, "claude-normal.jsonl")
	prefix := fixture(t, "claude-trailing-partial.jsonl") // real byte prefix of claude-normal
	p := h.writeClaude("-p", uuidNormal, full)
	h.runOnce()

	// Truncate below the cursor: rewrite the file as its own real prefix.
	if err := os.WriteFile(p, prefix, 0o644); err != nil {
		t.Fatal(err)
	}
	before := len(h.store.puts("claude", uuidNormal, uuidNormal))
	h.runOnce()
	h.runOnce() // a second pass must be a no-op, not another reset cycle
	after := h.store.puts("claude", uuidNormal, uuidNormal)

	zeroPuts := 0
	for _, off := range after[before:] {
		if off == 0 {
			zeroPuts++
		}
	}
	if zeroPuts != 1 {
		t.Fatalf("want exactly one reset re-ship from offset 0 after truncation, got %d (puts after truncation: %v)", zeroPuts, after[before:])
	}
	c, _ := h.cursor(wire.ToolClaude, uuidNormal)
	if c.Offset != int64(len(prefix)) {
		t.Fatalf("cursor after truncation quiescence: %d, want local size %d (no runaway to store high-water)", c.Offset, len(prefix))
	}
	// Mirror keeps the longer original bytes (mirror is durable truth).
	h.assertMirror("claude", uuidNormal, full)
}

// --- S4: move across dirs mid-tail → no re-ship, bytes continue (AC #2) ----

func TestMoveAcrossDirsNoReship(t *testing.T) {
	h := newHarness(t)
	full := fixture(t, "claude-normal.jsonl")
	half := full[:20000]
	p := h.writeClaude("-proj-old", uuidNormal, half)
	h.runOnce()

	// Simulate /cd: move to another project dir, then the session appends.
	newDir := filepath.Join(h.roots.Claude, "-proj-new")
	os.MkdirAll(newDir, 0o755)
	newPath := filepath.Join(newDir, uuidNormal+".jsonl")
	if err := os.Rename(p, newPath); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(newPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(full[20000:])
	f.Close()

	before := len(h.store.puts("claude", uuidNormal, uuidNormal))
	h.runOnce()
	newPuts := h.store.puts("claude", uuidNormal, uuidNormal)[before:]
	for _, off := range newPuts {
		if off == 0 {
			t.Fatalf("re-ship from offset 0 after a move: puts %v (identity must survive the move)", newPuts)
		}
	}
	h.assertMirror("claude", uuidNormal, full)
	if h.store.generationCount("claude", uuidNormal, uuidNormal) != 1 {
		t.Fatal("a move must not open a new generation (no second session, no recreate)")
	}
}

// --- S5: deletion → cursor GC only, mirror retained (AC #2) ----------------

func TestDeletionGCsCursorKeepsMirror(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "codex-rollout-meta.jsonl")
	p := h.writeCodex("2026/06/26", uuidCodex, data)
	h.runOnce()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	before := len(h.store.puts("codex", uuidCodex, uuidCodex))
	h.runOnce()

	if _, ok := h.cursor(wire.ToolCodex, uuidCodex); ok {
		t.Fatal("cursor must be GC'd after deletion")
	}
	if got := len(h.store.puts("codex", uuidCodex, uuidCodex)); got != before {
		t.Fatalf("deletion must not trigger any re-ship (puts %d -> %d)", before, got)
	}
	h.assertMirror("codex", uuidCodex, data) // mirror outlives the source (I7)
}

// --- AC #3: same-path recreate ≥1KiB → fingerprint mismatch → reset --------

func TestRecreateAboveWindowFingerprintMismatchResets(t *testing.T) {
	h := newHarness(t)
	oldBytes := fixture(t, "claude-normal.jsonl")
	newBytes := fixture(t, "claude-resume-new-file.jsonl") // different real content, ≥1KiB
	if len(newBytes) < len(oldBytes) {
		// Recreate must NOT be smaller, so the size-regression rule cannot
		// fire first and the fingerprint path is what's exercised.
		t.Fatalf("fixture choice broken: new %d < old %d", len(newBytes), len(oldBytes))
	}
	p := h.writeClaude("-p", uuidNormal, oldBytes)
	h.runOnce()
	if err := os.WriteFile(p, newBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	h.runOnce()

	// The store must now hold a second generation carrying the new content;
	// generation 0 bytes stay intact (both histories preserved).
	if got := h.store.generationCount("claude", uuidNormal, uuidNormal); got != 2 {
		t.Fatalf("generations after recreate: %d, want 2", got)
	}
	if string(h.store.generationBytes("claude", uuidNormal, uuidNormal, 0)) != string(oldBytes) {
		t.Fatal("generation 0 bytes must remain the original history")
	}
	if string(h.store.generationBytes("claude", uuidNormal, uuidNormal, 1)) != string(newBytes) {
		t.Fatal("generation 1 must hold the complete recreated history from offset 0")
	}
	c, _ := h.cursor(wire.ToolClaude, uuidNormal)
	if c.Offset != int64(len(newBytes)) {
		t.Fatalf("cursor after recreate: %d, want %d", c.Offset, len(newBytes))
	}
}

// --- AC #3: recreate below window → size-regression rule fires first -------

func TestRecreateBelowWindowCaughtBySizeRegression(t *testing.T) {
	h := newHarness(t)
	oldBytes := fixture(t, "claude-normal.jsonl")
	small := []byte(`{"type":"summary","summary":"tiny recreated file"}` + "\n") // < 1KiB
	p := h.writeClaude("-p", uuidNormal, oldBytes)
	h.runOnce()
	if err := os.WriteFile(p, small, 0o644); err != nil {
		t.Fatal(err)
	}
	h.runOnce()
	h.runOnce() // must be quiescent, not looping

	// Below the window no fingerprint is sent; divergence at offset 0 runs
	// the byte-conflict handshake into a fresh generation.
	if got := h.store.generationCount("claude", uuidNormal, uuidNormal); got != 2 {
		t.Fatalf("generations: %d, want 2 (conflict handshake must have opened one)", got)
	}
	if string(h.store.generationBytes("claude", uuidNormal, uuidNormal, 1)) != string(small) {
		t.Fatal("new generation must hold the small recreated content")
	}
	c, _ := h.cursor(wire.ToolClaude, uuidNormal)
	if c.Offset != int64(len(small)) {
		t.Fatalf("cursor: %d, want %d", c.Offset, len(small))
	}
}

// --- AC #4: kill -9 mid-file + restart → no loss, replay absorbed ----------

func TestRestartReplayAbsorbed(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	h.writeClaude("-p", uuidNormal, data)
	h.runOnce()

	// Simulate the crash window: the store ACKed everything, but the
	// registry on disk still carries an older offset (cursor persistence is
	// ACK-then-advance, so a torn run can only be BEHIND the store).
	c, _ := h.cursor(wire.ToolClaude, uuidNormal)
	c.Offset = 20000
	if err := h.shipper.Registry.Put(c); err != nil {
		t.Fatal(err)
	}
	h.restart()
	h.runOnce()

	h.assertMirror("claude", uuidNormal, data)
	if got := h.store.generationCount("claude", uuidNormal, uuidNormal); got != 1 {
		t.Fatalf("replay after restart must not open generations, got %d", got)
	}
	c2, _ := h.cursor(wire.ToolClaude, uuidNormal)
	if c2.Offset != int64(len(data)) {
		t.Fatalf("cursor after replay absorb: %d, want %d", c2.Offset, len(data))
	}
}

// --- AC #4: store unreachable → cursor holds, no local queue ---------------

func TestStoreUnreachableHoldsPosition(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	h.writeClaude("-p", uuidNormal, data)
	h.store.unavailable = true

	err := h.shipper.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce against an unavailable store must report the hold")
	}
	if c, ok := h.cursor(wire.ToolClaude, uuidNormal); ok && c.Offset != 0 {
		t.Fatalf("cursor advanced without a durable ACK: %+v", c)
	}

	// Store comes back: catch up losslessly from the source file (the only
	// buffer).
	h.store.unavailable = false
	h.runOnce()
	h.assertMirror("claude", uuidNormal, data)
}

// Transport-level unreachability (connection refused) must behave exactly
// like store_unavailable: hold, no advance, catch up later.
func TestStoreConnectionRefusedHoldsPosition(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	h.writeClaude("-p", uuidNormal, data)
	goodURL := h.srv.URL
	h.shipper.Client.BaseURL = "http://127.0.0.1:1" // nothing listens here

	if err := h.shipper.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce with unreachable store must report the hold")
	}
	if c, ok := h.cursor(wire.ToolClaude, uuidNormal); ok && c.Offset != 0 {
		t.Fatalf("cursor advanced without a durable ACK: %+v", c)
	}
	h.shipper.Client.BaseURL = goodURL
	h.runOnce()
	h.assertMirror("claude", uuidNormal, data)
}

// --- AC #5: corrupt registry → rebuild via rescan + recovery GET -----------

func TestCorruptRegistryRebuildsViaRecovery(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "claude-normal.jsonl")
	h.writeClaude("-p", uuidNormal, data)
	h.runOnce()
	putsBefore := len(h.store.puts("claude", uuidNormal, uuidNormal))

	// Corrupt the registry on disk and restart.
	h.shipper.Registry.Close()
	if err := os.WriteFile(filepath.Join(h.stateDir, "cursors.json"), []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.openShipper()
	if !h.shipper.Registry.NeedsRecovery {
		t.Fatal("corrupt registry must be flagged for recovery rebuild")
	}
	h.runOnce()

	// Recovery GET must have restored the cursor at the store high-water:
	// nothing re-ships (at most a probe), and the cursor is exact.
	c, ok := h.cursor(wire.ToolClaude, uuidNormal)
	if !ok || c.Offset != int64(len(data)) {
		t.Fatalf("rebuilt cursor: %+v ok=%v, want offset %d", c, ok, len(data))
	}
	for _, off := range h.store.puts("claude", uuidNormal, uuidNormal)[putsBefore:] {
		if off == 0 {
			t.Fatal("rebuild must not re-ship the world from offset 0; recovery GET carries the high-water")
		}
	}
	// The corrupt file was set aside, never deleted.
	aside, _ := filepath.Glob(filepath.Join(h.stateDir, "cursors.json.unreadable-*"))
	if len(aside) != 1 {
		t.Fatalf("unreadable registry must be renamed aside (found %v)", aside)
	}
}

// --- AC #5: higher schema_generation → typed, non-destructive refusal ------

func TestNewerRegistryGenerationRefusedNonDestructively(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "cursors.json")
	content := []byte(`{"schema_generation": 99, "cursors": {}}`)
	if err := os.WriteFile(regPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := OpenRegistry(dir)
	var nre *NewerRegistryError
	if !errorsAs(err, &nre) {
		t.Fatalf("want NewerRegistryError, got %v", err)
	}
	msg := strings.ToLower(err.Error())
	for _, banned := range []string{"delete", "remove", "rm ", "wipe", "clear the registry"} {
		if strings.Contains(msg, banned) {
			t.Fatalf("refusal text must never advise destroying the registry (herder-incident lesson); got: %q", err.Error())
		}
	}
	for _, required := range []string{"newer", "generation"} {
		if !strings.Contains(msg, required) {
			t.Fatalf("refusal text must name the cause; missing %q in %q", required, err.Error())
		}
	}
	after, err2 := os.ReadFile(regPath)
	if err2 != nil || string(after) != string(content) {
		t.Fatal("refusal must leave the registry file byte-identical")
	}
}

// --- registry lock: one shipper per user ------------------------------------

func TestRegistrySingleInstanceLock(t *testing.T) {
	dir := t.TempDir()
	r1, err := OpenRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r1.Close()
	_, err = OpenRegistry(dir)
	var lre *LockedRegistryError
	if !errorsAs(err, &lre) {
		t.Fatalf("second open must refuse with LockedRegistryError, got %v", err)
	}
}

// --- byte-conflict handshake end-to-end (wire doc catalog rows 409/423) ----

func TestByteConflictHandshakeOpensGenerationThenPoisons(t *testing.T) {
	h := newHarness(t)
	// Seed the store with history that the local file diverges from beyond
	// the fingerprint window: same real first 20,000 bytes (same
	// fingerprint), different tail.
	orig := fixture(t, "claude-normal.jsonl")
	local := append(append([]byte(nil), orig[:20000]...), []byte(`{"type":"user","message":"diverged tail"}`+"\n")...)
	fp := fingerprintOf(t, orig)
	h.store.seed("claude", uuidNormal, uuidNormal, fp, orig)

	h.writeClaude("-p", uuidNormal, local)
	h.runOnce()

	// Handshake: byte_conflict → retry once → generation_opened → re-ship
	// from 0; generation 1 carries the complete divergent history.
	if got := h.store.generationCount("claude", uuidNormal, uuidNormal); got != 2 {
		t.Fatalf("generations: %d, want 2", got)
	}
	if string(h.store.generationBytes("claude", uuidNormal, uuidNormal, 0)) != string(orig) {
		t.Fatal("generation 0 must be untouched")
	}
	if string(h.store.generationBytes("claude", uuidNormal, uuidNormal, 1)) != string(local) {
		t.Fatal("generation 1 must hold the complete local history")
	}
}

func TestPoisonedFileParkedOthersKeepShipping(t *testing.T) {
	h := newHarness(t)
	poisonedData := fixture(t, "claude-normal.jsonl")
	okData := fixture(t, "codex-rollout-meta.jsonl")
	h.writeClaude("-p", uuidNormal, poisonedData)
	h.writeCodex("2026/06/26", uuidCodex, okData)
	h.store.seed("claude", uuidNormal, uuidNormal, "", nil)
	h.store.setPoisoned("claude", uuidNormal, uuidNormal)

	h.runOnce()

	c, ok := h.cursor(wire.ToolClaude, uuidNormal)
	if !ok || !c.Poisoned {
		t.Fatalf("poisoned file must be parked with a frozen, flagged cursor: %+v ok=%v", c, ok)
	}
	putsBefore := len(h.store.puts("claude", uuidNormal, uuidNormal))
	h.runOnce()
	if got := len(h.store.puts("claude", uuidNormal, uuidNormal)); got != putsBefore {
		t.Fatal("poisoned file must not be retried")
	}
	h.assertMirror("codex", uuidCodex, okData) // others keep shipping
}

// --- helpers ----------------------------------------------------------------

func fingerprintOf(t *testing.T, data []byte) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "fp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fp, ready, err := Fingerprint(tmp)
	if err != nil || !ready {
		t.Fatalf("fingerprint: ready=%v err=%v", ready, err)
	}
	return fp
}

func errorsAs(err error, target any) bool {
	if err == nil {
		return false
	}
	switch tt := target.(type) {
	case **NewerRegistryError:
		e, ok := err.(*NewerRegistryError)
		if ok {
			*tt = e
		}
		return ok
	case **LockedRegistryError:
		e, ok := err.(*LockedRegistryError)
		if ok {
			*tt = e
		}
		return ok
	}
	return false
}

// --- daemon loop: fsnotify hint ships a new file without waiting a rescan --

func TestRunDaemonShipsNewFileOnHint(t *testing.T) {
	h := newHarness(t)
	h.shipper.Rescan = 30 * time.Second // force reliance on the fsnotify hint
	os.MkdirAll(filepath.Join(h.roots.Claude, "-p"), 0o755)
	os.MkdirAll(h.roots.Codex, 0o755)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.shipper.Run(ctx) }()
	time.Sleep(300 * time.Millisecond) // let the watcher arm

	data := fixture(t, "claude-normal.jsonl")
	h.writeClaude("-p", uuidNormal, data)

	deadline := time.After(10 * time.Second)
	for {
		if string(h.store.mirrorBytes("claude", uuidNormal, uuidNormal)) == string(data) {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("daemon did not ship the new file from the fsnotify hint within 10s")
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("daemon exit: %v", err)
	}
}
