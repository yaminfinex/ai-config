//go:build linux

package ship

// Characterization tests for the /proc SESSION_OWNER correlation (U9, spec
// §4.2), written before the correlator per the plan's failing-test-first
// note. The kernel surface is reproduced as a fixture tree: status/comm/
// environ as regular files, cwd and fd entries as real symlinks — the same
// shapes the live scan reads, minus the kernel.

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sesh/internal/wire"
)

type fakeProc struct {
	t    testing.TB
	root string
}

func newFakeProc(t testing.TB) *fakeProc {
	t.Helper()
	return &fakeProc{t: t, root: t.TempDir()}
}

// add writes one /proc/<pid> entry. env == nil means no environ file at all;
// an "SESSION_OWNER" key absent from env means an environ without the
// variable. openFiles become fd/N symlinks.
func (f *fakeProc) add(pid, ppid, uid int, comm, cwd string, env map[string]string, openFiles ...string) {
	f.t.Helper()
	dir := filepath.Join(f.root, fmt.Sprint(pid))
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		f.t.Fatal(err)
	}
	status := fmt.Sprintf("Name:\t%s\nUmask:\t0002\nState:\tS (sleeping)\nPPid:\t%d\nUid:\t%d\t%d\t%d\t%d\n", comm, ppid, uid, uid, uid, uid)
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o644); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
		f.t.Fatal(err)
	}
	if cwd != "" {
		if err := os.Symlink(cwd, filepath.Join(dir, "cwd")); err != nil {
			f.t.Fatal(err)
		}
	}
	if env != nil {
		var sb strings.Builder
		for k, v := range env {
			sb.WriteString(k + "=" + v + "\x00")
		}
		if err := os.WriteFile(filepath.Join(dir, "environ"), []byte(sb.String()), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	for i, of := range openFiles {
		if err := os.Symlink(of, filepath.Join(dir, "fd", fmt.Sprint(3+i))); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fakeProc) correlator() *procCorrelator {
	return &procCorrelator{Root: f.root, UID: os.Getuid()}
}

func discoveredCodex(t *testing.T, dir string) Discovered {
	t.Helper()
	return discoveredCodexUUID(t, dir, uuidCodex)
}

func discoveredCodexUUID(t *testing.T, dir, uuid string) Discovered {
	t.Helper()
	p := filepath.Join(dir, "rollout-2026-06-26T02-43-06-"+uuid+".jsonl")
	if err := os.WriteFile(p, fixture(t, "codex-rollout-meta.jsonl"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Discovered{Identity: Identity{Tool: wire.ToolCodex, SessionID: uuid, FileUUID: uuid}, Path: p}
}

// discoveredClaude places the session file in a project dir named by the
// claude munge of cohortCwd (every byte outside [A-Za-z0-9] becomes '-'),
// which is the cohort key the correlator matches process cwds against.
func discoveredClaude(t *testing.T, base, cohortCwd string) Discovered {
	t.Helper()
	dir := filepath.Join(base, mungeCwd(cohortCwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, uuidNormal+".jsonl")
	if err := os.WriteFile(p, fixture(t, "claude-normal.jsonl"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Discovered{Identity: Identity{Tool: wire.ToolClaude, SessionID: uuidNormal, FileUUID: uuidNormal}, Path: p}
}

// --- S6a: codex is fd-exact --------------------------------------------------

func TestCodexOwnerExactViaOpenFD(t *testing.T) {
	fp := newFakeProc(t)
	d := discoveredCodex(t, t.TempDir())
	fp.add(100, 1, os.Getuid(), "codex", "/anywhere", map[string]string{"SESSION_OWNER": "alice", "HOME": "/home/x"}, d.Path)
	// An unrelated same-uid process holding other files must not interfere.
	fp.add(200, 1, os.Getuid(), "vim", "/anywhere", map[string]string{"SESSION_OWNER": "bob"}, filepath.Join(t.TempDir(), "other.txt"))

	owners := fp.correlator().CorrelateAll([]Discovered{d})
	if got := owners[d.Identity.Key()]; got != "alice" {
		t.Fatalf("codex owner = %q, want alice (exact fd join)", got)
	}
}

func TestCodexOwnerLeafHolderWins(t *testing.T) {
	fp := newFakeProc(t)
	d := discoveredCodex(t, t.TempDir())
	// Parent and child both hold the rollout open (inherited fd); the leaf
	// (child) names the owner.
	fp.add(100, 1, os.Getuid(), "sh", "/anywhere", map[string]string{"SESSION_OWNER": "parent-shell"}, d.Path)
	fp.add(101, 100, os.Getuid(), "codex", "/anywhere", map[string]string{"SESSION_OWNER": "alice"}, d.Path)

	owners := fp.correlator().CorrelateAll([]Discovered{d})
	if got := owners[d.Identity.Key()]; got != "alice" {
		t.Fatalf("codex owner = %q, want alice (leaf holder)", got)
	}
}

func TestCodexOwnerAbsentWhenNoHolderOrNoVariable(t *testing.T) {
	fp := newFakeProc(t)
	d := discoveredCodex(t, t.TempDir())
	// Holder exists but its environ has no SESSION_OWNER: honest absence.
	fp.add(100, 1, os.Getuid(), "codex", "/anywhere", map[string]string{"HOME": "/home/x"}, d.Path)

	owners := fp.correlator().CorrelateAll([]Discovered{d})
	if got, ok := owners[d.Identity.Key()]; ok {
		t.Fatalf("owner = %q, want absence (no SESSION_OWNER in holder environ)", got)
	}
	// Dead session: no process holds the file at all.
	owners = newFakeProc(t).correlator().CorrelateAll([]Discovered{d})
	if got, ok := owners[d.Identity.Key()]; ok {
		t.Fatalf("owner = %q, want absence (no holder)", got)
	}
}

// --- S6b: claude cohort is unanimous-or-absent -------------------------------

func TestClaudeCohortUnanimousOrAbsent(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "work.tree")
	d := discoveredClaude(t, base, cwd)

	t.Run("single candidate stamps", func(t *testing.T) {
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "claude", cwd, map[string]string{"SESSION_OWNER": "bob"})
		if got := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; got != "bob" {
			t.Fatalf("owner = %q, want bob", got)
		}
	})
	t.Run("two agreeing candidates stamp", func(t *testing.T) {
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "claude", cwd, map[string]string{"SESSION_OWNER": "bob"})
		fp.add(101, 1, os.Getuid(), "claude", cwd, map[string]string{"SESSION_OWNER": "bob"})
		if got := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; got != "bob" {
			t.Fatalf("owner = %q, want bob (unanimous)", got)
		}
	})
	t.Run("disagreeing candidates yield absence", func(t *testing.T) {
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "claude", cwd, map[string]string{"SESSION_OWNER": "carol"})
		fp.add(101, 1, os.Getuid(), "claude", cwd, map[string]string{"SESSION_OWNER": "dave"})
		if got, ok := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; ok {
			t.Fatalf("owner = %q, want honest absence (same-cwd collision)", got)
		}
	})
	t.Run("candidate without the variable breaks unanimity", func(t *testing.T) {
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "claude", cwd, map[string]string{"SESSION_OWNER": "carol"})
		fp.add(101, 1, os.Getuid(), "claude", cwd, map[string]string{"HOME": "/home/x"})
		if got, ok := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; ok {
			t.Fatalf("owner = %q, want absence", got)
		}
	})
	t.Run("munge-colliding distinct cwds yield absence even when owners agree", func(t *testing.T) {
		// The project-dir slug is lossy: /x/work.tree and /x/work-tree munge
		// identically, so candidates from BOTH map to this session's slug
		// while only one cwd can be the session's real cohort (U9 review,
		// HIGH). Same owner on both sides is the discriminating case — a
		// slug-keyed cohort would happily stamp it.
		collided := strings.Replace(cwd, ".", "-", 1)
		if mungeCwd(collided) != mungeCwd(cwd) || collided == cwd {
			t.Fatalf("test self-check: %q and %q must be distinct munge-colliding cwds", cwd, collided)
		}
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "claude", cwd, map[string]string{"SESSION_OWNER": "erin"})
		fp.add(101, 1, os.Getuid(), "claude", collided, map[string]string{"SESSION_OWNER": "erin"})
		if got, ok := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; ok {
			t.Fatalf("owner = %q, want absence (two distinct cwds behind one slug: cohort unresolvable)", got)
		}
	})
	t.Run("other cwd and other comm are not candidates", func(t *testing.T) {
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "claude", cwd, map[string]string{"SESSION_OWNER": "bob"})
		fp.add(101, 1, os.Getuid(), "claude", "/elsewhere", map[string]string{"SESSION_OWNER": "carol"})
		fp.add(102, 1, os.Getuid(), "vim", cwd, map[string]string{"SESSION_OWNER": "dave"})
		if got := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; got != "bob" {
			t.Fatalf("owner = %q, want bob (cohort excludes other cwd/comm)", got)
		}
	})
}

// --- grok cohort: exact percent-decoded cwd, unanimous-or-absent -------------

// discoveredGrok places a session transcript under a percent-encoded cwd
// group, the live ~/.grok/sessions layout.
func discoveredGrok(t *testing.T, base, cohortCwd string) Discovered {
	t.Helper()
	dir := filepath.Join(base, url.PathEscape(cohortCwd), uuidGrokB)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, grokTranscriptName)
	if err := os.WriteFile(p, fixture(t, "grok-chat-history.jsonl"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Discovered{Identity: Identity{Tool: wire.ToolGrok, SessionID: uuidGrokB, FileUUID: uuidGrokB}, Path: p}
}

func TestGrokCohortUnanimousOrAbsentOnExactCwd(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "work.tree")
	d := discoveredGrok(t, base, cwd)

	t.Run("single candidate stamps", func(t *testing.T) {
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "grok", cwd, map[string]string{"SESSION_OWNER": "bob"})
		if got := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; got != "bob" {
			t.Fatalf("owner = %q, want bob", got)
		}
	})
	t.Run("disagreeing candidates yield absence", func(t *testing.T) {
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "grok", cwd, map[string]string{"SESSION_OWNER": "carol"})
		fp.add(101, 1, os.Getuid(), "grok", cwd, map[string]string{"SESSION_OWNER": "dave"})
		if got, ok := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; ok {
			t.Fatalf("owner = %q, want honest absence (same-cwd collision)", got)
		}
	})
	t.Run("candidate without the variable breaks unanimity", func(t *testing.T) {
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "grok", cwd, map[string]string{"SESSION_OWNER": "carol"})
		fp.add(101, 1, os.Getuid(), "grok", cwd, map[string]string{"HOME": "/home/x"})
		if got, ok := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; ok {
			t.Fatalf("owner = %q, want absence", got)
		}
	})
	t.Run("munge-colliding cwd is NOT a grok collision (encoding is exact)", func(t *testing.T) {
		// /x/work.tree and /x/work-tree collide under the claude slug but
		// percent-encode distinctly; the grok cohort keys on the exact
		// decoded cwd, so the other directory's process is simply not a
		// candidate and the real cohort stamps.
		other := strings.Replace(cwd, ".", "-", 1)
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "grok", cwd, map[string]string{"SESSION_OWNER": "erin"})
		fp.add(101, 1, os.Getuid(), "grok", other, map[string]string{"SESSION_OWNER": "frank"})
		if got := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; got != "erin" {
			t.Fatalf("owner = %q, want erin (exact-cwd cohort excludes the lookalike)", got)
		}
	})
	t.Run("other comm is not a candidate", func(t *testing.T) {
		fp := newFakeProc(t)
		fp.add(100, 1, os.Getuid(), "grok", cwd, map[string]string{"SESSION_OWNER": "bob"})
		fp.add(101, 1, os.Getuid(), "vim", cwd, map[string]string{"SESSION_OWNER": "carol"})
		if got := fp.correlator().CorrelateAll([]Discovered{d})[d.Identity.Key()]; got != "bob" {
			t.Fatalf("owner = %q, want bob (cohort excludes other comm)", got)
		}
	})
}

// --- S7/I9: the cross-user wall — other uids skipped before any read ---------

func TestOtherUIDSkippedSilentlyBeforeAnyRead(t *testing.T) {
	fp := newFakeProc(t)
	d := discoveredCodex(t, t.TempDir())
	// Another uid's process holds the same rollout with a READABLE environ:
	// if the correlator consulted it, "mallory" would leak into the stamp.
	// The uid gate must reject it from the status line alone.
	fp.add(300, 1, os.Getuid()+1, "codex", "/anywhere", map[string]string{"SESSION_OWNER": "mallory"}, d.Path)
	fp.add(100, 1, os.Getuid(), "codex", "/anywhere", map[string]string{"SESSION_OWNER": "alice"}, d.Path)

	owners := fp.correlator().CorrelateAll([]Discovered{d})
	if got := owners[d.Identity.Key()]; got != "alice" {
		t.Fatalf("owner = %q, want alice (other-uid holder ignored)", got)
	}

	// On a live /proc the other uid's environ is unreadable (0400); a dying
	// same-uid process races the same way. Either read failure is silent
	// absence, never an error.
	fp2 := newFakeProc(t)
	d2 := discoveredCodex(t, t.TempDir())
	fp2.add(400, 1, os.Getuid(), "codex", "/anywhere", map[string]string{"SESSION_OWNER": "gone"}, d2.Path)
	if err := os.Chmod(filepath.Join(fp2.root, "400", "environ"), 0o000); err != nil {
		t.Fatal(err)
	}
	if os.Getuid() == 0 {
		t.Skip("running as root: chmod 000 does not make the file unreadable")
	}
	owners = fp2.correlator().CorrelateAll([]Discovered{d2})
	if got, ok := owners[d2.Identity.Key()]; ok {
		t.Fatalf("owner = %q, want silent absence on unreadable environ", got)
	}
}

// A process table that vanishes mid-scan (procfs races) must never error the
// pass: correlation is best-effort enrichment, shipping never depends on it.
func TestCorrelatorToleratesVanishingEntries(t *testing.T) {
	fp := newFakeProc(t)
	d := discoveredCodex(t, t.TempDir())
	// Entry with a status file only — no comm, no cwd, no fd dir.
	if err := os.MkdirAll(filepath.Join(fp.root, "500"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fp.root, "500", "status"), []byte(fmt.Sprintf("PPid:\t1\nUid:\t%d\t0\t0\t0\n", os.Getuid())), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-numeric entries (self, sys, ...) are ignored.
	if err := os.MkdirAll(filepath.Join(fp.root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if owners := fp.correlator().CorrelateAll([]Discovered{d}); len(owners) != 0 {
		t.Fatalf("owners = %v, want none", owners)
	}
}

func TestCorrelationCacheHitAndExpiryObservePIDChurn(t *testing.T) {
	fp := newFakeProc(t)
	d := discoveredCodex(t, t.TempDir())
	fp.add(100, 1, os.Getuid(), "codex", "/work", map[string]string{"SESSION_OWNER": "alice"}, d.Path)
	c := fp.correlator()
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	if got := c.CorrelateAll([]Discovered{d})[d.Identity.Key()]; got != "alice" {
		t.Fatalf("initial owner = %q, want alice", got)
	}
	if c.scanCount != 1 {
		t.Fatalf("initial scans = %d, want 1", c.scanCount)
	}

	if err := os.RemoveAll(filepath.Join(fp.root, "100")); err != nil {
		t.Fatal(err)
	}
	fp.add(101, 1, os.Getuid(), "codex", "/work", map[string]string{"SESSION_OWNER": "bob"}, d.Path)
	now = now.Add(9 * time.Second)
	if got := c.CorrelateAll([]Discovered{d})[d.Identity.Key()]; got != "alice" {
		t.Fatalf("cached owner = %q, want historical positive alice before expiry", got)
	}
	if c.scanCount != 1 {
		t.Fatalf("cache-hit scans = %d, want 1", c.scanCount)
	}

	now = now.Add(time.Second)
	if got := c.CorrelateAll([]Discovered{d})[d.Identity.Key()]; got != "bob" {
		t.Fatalf("owner after expiry and PID churn = %q, want bob", got)
	}
	if c.scanCount != 2 {
		t.Fatalf("expired scans = %d, want 2", c.scanCount)
	}
}

func TestCorrelationCacheRefreshesForNewIdentity(t *testing.T) {
	fp := newFakeProc(t)
	d1 := discoveredCodexUUID(t, t.TempDir(), "10000000-0000-4000-8000-000000000000")
	d2 := discoveredCodexUUID(t, t.TempDir(), "20000000-0000-4000-8000-000000000000")
	fp.add(100, 1, os.Getuid(), "codex", "/work/a", map[string]string{"SESSION_OWNER": "alice"}, d1.Path)
	c := fp.correlator()
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	c.CorrelateAll([]Discovered{d1})
	fp.add(200, 1, os.Getuid(), "codex", "/work/b", map[string]string{"SESSION_OWNER": "bob"}, d2.Path)
	now = now.Add(time.Second)
	owners := c.CorrelateAll([]Discovered{d1, d2})
	if got := owners[d2.Identity.Key()]; got != "bob" {
		t.Fatalf("new identity owner = %q, want immediate bob correlation", got)
	}
	if c.scanCount != 2 {
		t.Fatalf("scans after identity-set growth = %d, want 2 before TTL expiry", c.scanCount)
	}
}

func TestExpiredCorrelationAbsenceKeepsPersistedOwner(t *testing.T) {
	h := newHarness(t)
	data := fixture(t, "codex-rollout-meta.jsonl")
	path := h.writeCodex("2026/07/10", uuidCodex, data)
	fp := newFakeProc(t)
	fp.add(100, 1, os.Getuid(), "codex", "/work", map[string]string{"SESSION_OWNER": "alice"}, path)
	c := fp.correlator()
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	h.shipper.Correlate = c.CorrelateAll
	h.runOnce()

	if err := os.RemoveAll(filepath.Join(fp.root, "100")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	cacheHitBytes := append(append([]byte(nil), data...), '\n')
	if err := os.WriteFile(path, cacheHitBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	h.runOnce()
	h.assertMirror("codex", uuidCodex, cacheHitBytes)
	if c.scanCount != 1 {
		t.Fatalf("cache-hit pass performed %d proc sweeps, want 1 total while bytes still shipped", c.scanCount)
	}

	now = now.Add(defaultCorrelationTTL)
	afterExpiryBytes := append(append([]byte(nil), cacheHitBytes...), '\n')
	if err := os.WriteFile(path, afterExpiryBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	h.runOnce()
	cursor, _ := h.cursor(wire.ToolCodex, uuidCodex)
	if cursor.SessionOwner != "alice" {
		t.Fatalf("owner after process death = %q, want persisted alice", cursor.SessionOwner)
	}
	h.assertMirror("codex", uuidCodex, afterExpiryBytes)
	if c.scanCount != 2 {
		t.Fatalf("expired pass proc sweeps = %d, want 2 total", c.scanCount)
	}
	owners := h.store.owners("codex", uuidCodex, uuidCodex)
	if got := owners[len(owners)-1]; got != "alice" {
		t.Fatalf("owner shipped after process death = %q, want persisted alice", got)
	}
}

func BenchmarkCorrelationAcrossFivePasses(b *testing.B) {
	fp := newFakeProc(b)
	cwd := filepath.Join(b.TempDir(), "work.tree")
	fp.add(100, 1, os.Getuid(), "claude", cwd, map[string]string{"SESSION_OWNER": "alice"})
	for pid := 101; pid < 850; pid++ {
		fp.add(pid, 1, os.Getuid(), "worker", "/elsewhere", map[string]string{"HOME": "/tmp"})
	}
	discovered := make([]Discovered, 750)
	for i := range discovered {
		uuid := fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
		discovered[i] = Discovered{
			Identity: Identity{Tool: wire.ToolClaude, SessionID: uuid, FileUUID: uuid},
			Path:     filepath.Join("root", mungeCwd(cwd), uuid+".jsonl"),
		}
	}

	for _, tc := range []struct {
		name string
		ttl  time.Duration
	}{
		{name: "full-sweep-every-pass", ttl: time.Nanosecond},
		{name: "cached-ten-seconds", ttl: defaultCorrelationTTL},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
				c := fp.correlator()
				c.ttl = tc.ttl
				c.now = func() time.Time { return now }
				for pass := 0; pass < 5; pass++ {
					c.CorrelateAll(discovered)
					now = now.Add(2 * time.Second)
				}
			}
		})
	}
}
