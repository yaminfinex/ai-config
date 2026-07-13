package sidecarcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSafeStatuslineName(t *testing.T) {
	tests := map[string]bool{
		"worker-rive":    true,
		"task067-sumo":   true,
		"luna:BOXE":      true,
		"":               false,
		".":              false,
		"..":             false,
		"../escape":      false,
		"nested/name":    false,
		`nested\name`:    false,
		"almost..escape": false,
	}
	for name, wantOK := range tests {
		got, ok := safeStatuslineName(name)
		if ok != wantOK {
			t.Fatalf("safeStatuslineName(%q) ok=%v, want %v", name, ok, wantOK)
		}
		if ok && got != name {
			t.Fatalf("safeStatuslineName(%q) = %q, want unchanged", name, got)
		}
	}
}

func TestStatuslineSnapshotNameUsesBaseNameWhenPresent(t *testing.T) {
	got, ok := statuslineSnapshotName(hcomRow{Name: "task067-sumo", BaseName: "sumo"})
	if !ok || got != "sumo" {
		t.Fatalf("statuslineSnapshotName = %q, %v; want sumo, true", got, ok)
	}
	got, ok = statuslineSnapshotName(hcomRow{Name: "plain"})
	if !ok || got != "plain" {
		t.Fatalf("statuslineSnapshotName fallback = %q, %v; want plain, true", got, ok)
	}
	if got, ok = statuslineSnapshotName(hcomRow{Name: "safe", BaseName: "../escape"}); ok {
		t.Fatalf("unsafe base_name accepted as %q", got)
	}
}

func TestStatuslineSnapshotWriterWritesAtomicallyShapedFile(t *testing.T) {
	root := t.TempDir()
	w := newStatuslineSnapshotWriter(root)
	now := time.Unix(200, 0)

	w.writeRows([]hcomRow{{
		Name:        "worker-rive",
		UnreadCount: 3,
		StatusAgeS:  42,
	}}, now)

	path := filepath.Join(root, "statusline", "worker-rive.env")
	got := readFile(t, path)
	want := "HCOM_LIVE_NAME=worker-rive\nHCOM_UNREAD=3\nHCOM_LAST_TS=158\nHCOM_LAST_AGE_S=42\n"
	if got != want {
		t.Fatalf("snapshot = %q, want %q", got, want)
	}
	entries, err := os.ReadDir(filepath.Join(root, "statusline"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file remained after atomic replace: %s", entry.Name())
		}
	}
}

func TestStatuslineSnapshotWriterSkipsUnsafeNamesAndCleansOnlyTrackedFiles(t *testing.T) {
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(statusDir, "foreign.env")
	if err := os.WriteFile(foreign, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := newStatuslineSnapshotWriter(root)
	w.writeRows([]hcomRow{
		{Name: "worker-rive", UnreadCount: 1, StatusAgeS: 0},
		{Name: "../escape", UnreadCount: 9, StatusAgeS: 0},
	}, time.Unix(100, 0))
	if _, err := os.Stat(filepath.Join(root, "escape.env")); !os.IsNotExist(err) {
		t.Fatalf("unsafe name wrote outside statusline dir: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(statusDir, "worker-rive.env")); err != nil {
		t.Fatalf("tracked snapshot missing after write: %v", err)
	}

	w.writeRows(nil, time.Unix(101, 0))
	if _, err := os.Stat(filepath.Join(statusDir, "worker-rive.env")); err != nil {
		t.Fatalf("nil roster cleaned tracked snapshot after transient failure: %v", err)
	}

	w.writeRows([]hcomRow{}, time.Unix(101, 0))
	if _, err := os.Stat(filepath.Join(statusDir, "worker-rive.env")); !os.IsNotExist(err) {
		t.Fatalf("tracked stale snapshot still exists: err=%v", err)
	}
	if got := readFile(t, foreign); got != "keep\n" {
		t.Fatalf("foreign file changed during cleanup: %q", got)
	}
}

func TestStatuslineSnapshotWriterSkipsTimestampDriftWithinTick(t *testing.T) {
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(statusDir, "worker-rive.env")
	original := "HCOM_LIVE_NAME=worker-rive\nHCOM_UNREAD=5\nHCOM_LAST_TS=100\nHCOM_LAST_AGE_S=10\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	w := newStatuslineSnapshotWriter(root)
	w.writeRows([]hcomRow{{Name: "worker-rive", UnreadCount: 5, StatusAgeS: 0}}, time.Unix(102, 0))
	if got := readFile(t, path); got != original {
		t.Fatalf("timestamp drift within tolerance rewrote file: %q", got)
	}

	w.writeRows([]hcomRow{{Name: "worker-rive", UnreadCount: 6, StatusAgeS: 0}}, time.Unix(102, 0))
	if got := readFile(t, path); got == original || !strings.Contains(got, "HCOM_UNREAD=6\n") {
		t.Fatalf("unread change did not rewrite file: %q", got)
	}
}

func TestStatuslineSnapshotWriterPreservesContextMetrics(t *testing.T) {
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(statusDir, "worker-rive.env")
	original := strings.Join([]string{
		"HCOM_UNREAD=1",
		"HCOM_LAST_TS=90",
		"HCOM_LAST_AGE_S=10",
		"CTX_PCT=24",
		"CTX_TOKENS=61768",
		"CTX_SIZE=258400",
		"CTX_TS=100",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	w := newStatuslineSnapshotWriter(root)
	w.writeRows([]hcomRow{{Name: "worker-rive", UnreadCount: 2, StatusAgeS: 5}}, time.Unix(120, 0))

	got := readFile(t, path)
	for _, want := range []string{
		"HCOM_UNREAD=2\n",
		"HCOM_LAST_TS=115\n",
		"HCOM_LAST_AGE_S=5\n",
		"CTX_PCT=24\n",
		"CTX_TOKENS=61768\n",
		"CTX_SIZE=258400\n",
		"CTX_TS=100\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snapshot missing %q after hcom refresh: %q", want, got)
		}
	}
}

func TestStatuslineSnapshotWriterDropsInvalidContextPercent(t *testing.T) {
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(statusDir, "worker-rive.env")
	original := strings.Join([]string{
		"HCOM_UNREAD=1",
		"HCOM_LAST_TS=90",
		"HCOM_LAST_AGE_S=10",
		"CTX_PCT=Inf",
		"CTX_TOKENS=61768",
		"CTX_SIZE=258400",
		"CTX_TS=100",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	w := newStatuslineSnapshotWriter(root)
	w.writeRows([]hcomRow{{Name: "worker-rive", UnreadCount: 2, StatusAgeS: 5}}, time.Unix(120, 0))

	got := readFile(t, path)
	if strings.Contains(got, "CTX_PCT=") {
		t.Fatalf("invalid context percent preserved: %q", got)
	}
}

func TestStatuslineSnapshotWriterRewritesTimestampDriftBeyondTick(t *testing.T) {
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(statusDir, "worker-rive.env")
	original := "HCOM_UNREAD=5\nHCOM_LAST_TS=100\nHCOM_LAST_AGE_S=10\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	w := newStatuslineSnapshotWriter(root)
	w.writeRows([]hcomRow{{Name: "worker-rive", UnreadCount: 5, StatusAgeS: 0}}, time.Unix(103, 0))
	if got := readFile(t, path); got == original || !strings.Contains(got, "HCOM_UNREAD=5\n") || !strings.Contains(got, "HCOM_LAST_TS=103\n") {
		t.Fatalf("timestamp drift beyond tolerance did not rewrite with unchanged unread: %q", got)
	}
}

func TestStatuslineSnapshotWriterOmitsCollidedBaseName(t *testing.T) {
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(statusDir, "sumo.env")
	if err := os.WriteFile(path, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := newStatuslineSnapshotWriter(root)
	rows := []hcomRow{
		{Name: "task067-sumo", BaseName: "sumo", UnreadCount: 1, StatusAgeS: 0},
		{Name: "other-sumo", BaseName: "sumo", UnreadCount: 9, StatusAgeS: 0},
	}
	w.writeRows(rows, time.Unix(100, 0))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("collided base_name file still exists: err=%v", err)
	}

	if err := os.WriteFile(path, []byte("reappeared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.writeRows(rows, time.Unix(101, 0))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("persistent collision did not remove reappeared file: err=%v", err)
	}

	w.writeRows([]hcomRow{{Name: "task067-sumo", BaseName: "sumo", UnreadCount: 2, StatusAgeS: 0}}, time.Unix(102, 0))
	if got := readFile(t, path); !strings.Contains(got, "HCOM_UNREAD=2\n") {
		t.Fatalf("collision clear did not restore snapshot: %q", got)
	}
}

func TestStatuslineSnapshotWriterRecreatesCachedFileWhenMissing(t *testing.T) {
	root := t.TempDir()
	w := newStatuslineSnapshotWriter(root)
	row := hcomRow{Name: "worker-rive", UnreadCount: 1, StatusAgeS: 0}
	now := time.Unix(100, 0)
	w.writeRows([]hcomRow{row}, now)

	path := filepath.Join(root, "statusline", "worker-rive.env")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	w.writeRows([]hcomRow{row}, now)
	if got := readFile(t, path); !strings.Contains(got, "HCOM_LAST_TS=100\n") {
		t.Fatalf("missing cached file was not recreated: %q", got)
	}
}

func TestStatuslineSnapshotWriterUsesStableIdentityAndCleansLegacyNames(t *testing.T) {
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"former.env", "current.env"} {
		if err := os.WriteFile(filepath.Join(statusDir, name), []byte("HCOM_LIVE_NAME="+strings.TrimSuffix(name, ".env")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w := newStatuslineSnapshotWriter(root)
	row := hcomRow{Name: "former", BaseName: "current", UnreadCount: 2, StatusAgeS: 5}
	row.LaunchContext.PaneID = "pane-1"
	row.LaunchContext.ProcessID = "process-stable-0000"
	w.writeCorrelated(row, []hcomRow{row}, time.Unix(120, 0))

	got := readFile(t, filepath.Join(statusDir, "process-stable-0000.env"))
	if !strings.Contains(got, "HCOM_LIVE_NAME=current\n") {
		t.Fatalf("stable snapshot missing live name: %q", got)
	}
	if _, err := os.Stat(filepath.Join(statusDir, "former.env")); !os.IsNotExist(err) {
		t.Fatalf("owned pre-rename snapshot still exists: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(statusDir, "current.env")); err != nil {
		t.Fatalf("unowned current-name snapshot was removed: %v", err)
	}
}

func TestStatuslineSnapshotWriterDoesNotDeleteRecycledName(t *testing.T) {
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(statusDir, "former.env")
	if err := os.WriteFile(victim, []byte("HCOM_LIVE_NAME=former\nCTX_PCT=63\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mine := hcomRow{Name: "former", BaseName: "current"}
	mine.LaunchContext.ProcessID = "process-mine"
	other := hcomRow{Name: "former", BaseName: "former"}
	other.LaunchContext.ProcessID = "process-other"

	w := newStatuslineSnapshotWriter(root)
	w.writeRows([]hcomRow{mine, other}, time.Unix(99, 0))
	w.writeCorrelated(mine, []hcomRow{mine, other}, time.Unix(100, 0))
	if got := readFile(t, victim); !strings.Contains(got, "CTX_PCT=63\n") {
		t.Fatalf("recycled-name snapshot changed: %q", got)
	}
}

func TestStatuslineSnapshotWriterCleansTransitionOnlyOnce(t *testing.T) {
	root := t.TempDir()
	w := newStatuslineSnapshotWriter(root)
	row := hcomRow{Name: "former", BaseName: "current"}
	row.LaunchContext.ProcessID = "process-mine"
	w.writeCorrelated(row, []hcomRow{row}, time.Unix(100, 0))

	legacy := filepath.Join(root, "statusline", "former.env")
	if err := os.WriteFile(legacy, []byte("HCOM_LIVE_NAME=former\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.writeCorrelated(row, []hcomRow{row}, time.Unix(101, 0))
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("later tick repeated transition cleanup: %v", err)
	}
}

func TestSidecarSnapshotCorrelationSkipsForkParent(t *testing.T) {
	root := t.TempDir()
	s := &sidecar{
		paneID:              "pane-child",
		lifecycleMode:       "fork",
		parentSessionID:     "session-parent",
		correlatedProcessID: "process-parent",
		statuslineSnapshots: newStatuslineSnapshotWriter(root),
	}
	parent := hcomRow{Name: "parent", SessionID: "session-parent"}
	parent.LaunchContext.ProcessID = "process-parent"
	parent.LaunchContext.PaneID = "pane-parent"

	s.writeStatuslineSnapshots([]hcomRow{parent})
	if _, err := os.Stat(filepath.Join(root, "statusline", "process-parent.env")); !os.IsNotExist(err) {
		t.Fatalf("fork child wrote parent-keyed snapshot: err=%v", err)
	}
}

func TestSidecarSnapshotCorrelationPrefersPane(t *testing.T) {
	root := t.TempDir()
	s := &sidecar{
		paneID:              "pane-mine",
		correlatedProcessID: "process-stale",
		statuslineSnapshots: newStatuslineSnapshotWriter(root),
	}
	stale := hcomRow{Name: "stale"}
	stale.LaunchContext.ProcessID = "process-stale"
	mine := hcomRow{Name: "current", UnreadCount: 1}
	mine.LaunchContext.PaneID = "pane-mine"
	mine.LaunchContext.ProcessID = "process-current"

	s.writeStatuslineSnapshots([]hcomRow{stale, mine})
	got := readFile(t, filepath.Join(root, "statusline", "process-current.env"))
	if !strings.Contains(got, "HCOM_LIVE_NAME=current\n") {
		t.Fatalf("pane-correlated snapshot has wrong identity: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "statusline", "process-stale.env")); !os.IsNotExist(err) {
		t.Fatalf("earlier process match won over pane match: err=%v", err)
	}
}

func TestTransientReleasePreservesSnapshot(t *testing.T) {
	root := t.TempDir()
	w := newStatuslineSnapshotWriter(root)
	row := hcomRow{Name: "current"}
	row.LaunchContext.ProcessID = "process-stable-0000"
	w.writeCorrelated(row, []hcomRow{row}, time.Unix(100, 0))
	path := filepath.Join(root, "statusline", "process-stable-0000.env")
	s := &sidecar{statuslineSnapshots: w, socketPath: filepath.Join(root, "missing.sock")}

	s.release(false)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("transient release removed snapshot: %v", err)
	}
	s.release(true)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("genuine release retained snapshot: err=%v", err)
	}
}

func TestSidecarReleaseRemovesOwnStatuslineSnapshot(t *testing.T) {
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(statusDir, "other.env")
	if err := os.WriteFile(foreign, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HCOM_DIR", root)
	w := newStatuslineSnapshotWriter(root)
	row := hcomRow{Name: "current"}
	row.LaunchContext.ProcessID = "process-stable-0000"
	w.writeCorrelated(row, []hcomRow{row}, time.Unix(100, 0))
	stable := filepath.Join(statusDir, "process-stable-0000.env")
	(&sidecar{statuslineSnapshots: w}).removeOwnStatuslineSnapshot()
	if got := readFile(t, foreign); got != "keep\n" {
		t.Fatalf("release cleanup changed foreign snapshot: %q", got)
	}
	if _, err := os.Stat(stable); !os.IsNotExist(err) {
		t.Fatalf("stable statusline snapshot still exists after release cleanup: err=%v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
