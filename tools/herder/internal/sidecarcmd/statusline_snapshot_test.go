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
		if err := os.WriteFile(filepath.Join(statusDir, name), []byte("stale\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HERDER_GUID", "guid-stable-0000")
	t.Setenv("HERDR_PANE_ID", "pane-1")
	t.Setenv("HCOM_INSTANCE_NAME", "former")

	w := newStatuslineSnapshotWriter(root)
	row := hcomRow{Name: "current", UnreadCount: 2, StatusAgeS: 5}
	row.LaunchContext.PaneID = "pane-1"
	w.writeRows([]hcomRow{row}, time.Unix(120, 0))

	got := readFile(t, filepath.Join(statusDir, "guid-stable-0000.env"))
	if !strings.Contains(got, "HCOM_LIVE_NAME=current\n") {
		t.Fatalf("stable snapshot missing live name: %q", got)
	}
	for _, name := range []string{"former.env", "current.env"} {
		if _, err := os.Stat(filepath.Join(statusDir, name)); !os.IsNotExist(err) {
			t.Fatalf("legacy snapshot %s still exists: err=%v", name, err)
		}
	}
}

func TestSidecarReleaseRemovesOwnStatuslineSnapshot(t *testing.T) {
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(statusDir, "sumo.env")
	if err := os.WriteFile(path, []byte("owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(statusDir, "other.env")
	if err := os.WriteFile(foreign, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HCOM_DIR", root)
	t.Setenv("HCOM_INSTANCE_NAME", "sumo")
	t.Setenv("HERDER_GUID", "guid-stable-0000")
	stable := filepath.Join(statusDir, "guid-stable-0000.env")
	if err := os.WriteFile(stable, []byte("owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	(&sidecar{}).removeOwnStatuslineSnapshot()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("own statusline snapshot still exists after release cleanup: err=%v", err)
	}
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
