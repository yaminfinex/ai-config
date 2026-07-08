package spawncmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"ai-config/tools/herder/internal/registry"
)

// resolveBus calls resolveSpawnerBus discarding the ambiguity signal + warning,
// for the many cases that assert only the resolved name.
func resolveBus(path, notifyTo, spawnedBy, pane, term, childDir string) string {
	name, _ := resolveSpawnerBus(path, notifyTo, spawnedBy, pane, term, childDir, io.Discard)
	return name
}

func TestHcomEntryAcceptsNumericCreatedAt(t *testing.T) {
	var entries []hcomEntry
	data := []byte(`[{"name":"smoke-p5-tuna","tag":"smoke-p5","directory":"/tmp","created_at":1782979094.0}]`)
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := string(entries[0].CreatedAt); got != "1782979094.0" {
		t.Fatalf("CreatedAt = %q, want numeric value preserved", got)
	}
}

func TestResolveSpawnerBusMatchesEnrolledPane(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	rows := []string{
		// enrolled orchestrator: pane/terminal identity, bus name, NO spawner guid in play
		`{"guid":"guid-hera","short_guid":"guid-her","label":"orchestrator","pane_id":"p_orch","terminal_id":"term_ORCH","hcom_name":"hera","status":"active"}`,
		// closed row holding the SAME pane id from an older session must not win
		`{"guid":"guid-old","short_guid":"guid-old","label":"old","pane_id":"p_stale","terminal_id":"term_STALE","hcom_name":"stale-name","status":"closed"}`,
	}
	for _, row := range rows {
		if err := registry.Append(path, []byte(row)); err != nil {
			t.Fatal(err)
		}
	}

	// spawner identified only by its pane id (the enrolled case: spawnedBy=user)
	if got := resolveBus(path, "", "user", "p_orch", "", ""); got != "hera" {
		t.Fatalf("pane match = %q, want hera", got)
	}
	// terminal id fallback (notifyTo auto-resolved to the spawner's terminal)
	if got := resolveBus(path, "term_ORCH", "user", "", "", ""); got != "hera" {
		t.Fatalf("terminal match via notifyTo = %q, want hera", got)
	}
	// closed rows never resolve by pane coordinates
	if got := resolveBus(path, "", "user", "p_stale", "", ""); got != "" {
		t.Fatalf("closed pane match = %q, want empty", got)
	}
	// guid resolution still wins first
	if got := resolveBus(path, "", "guid-hera", "", "", ""); got != "hera" {
		t.Fatalf("guid match = %q, want hera", got)
	}
}

func TestResolveSpawnerBusAcceptsBusNames(t *testing.T) {
	// Stub hcom on PATH so liveOnBus is hermetic: the bus knows only lone-wolf.
	stubDir := t.TempDir()
	stub := "#!/bin/sh\necho '[{\"name\":\"lone-wolf\",\"tag\":\"x\",\"directory\":\"/d\",\"created_at\":\"2026-01-01T00:00:00Z\",\"launch_context\":{\"pane_id\":\"p_9\"}}]'\n"
	if err := os.WriteFile(filepath.Join(stubDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	path := filepath.Join(t.TempDir(), "registry.jsonl")
	rows := []string{
		`{"guid":"guid-hera","short_guid":"guid-her","label":"orchestrator","pane_id":"p_orch","terminal_id":"term_ORCH","hcom_name":"hera","status":"active"}`,
		`{"guid":"guid-old","short_guid":"guid-old","label":"old","pane_id":"p_stale","terminal_id":"term_STALE","hcom_name":"ghost","status":"closed"}`,
	}
	for _, row := range rows {
		if err := registry.Append(path, []byte(row)); err != nil {
			t.Fatal(err)
		}
	}

	// --notify-to may BE a bus name: an ACTIVE row's hcom_name matches (TASK-023)
	if got := resolveBus(path, "hera", "user", "", "", "/no-such-bus"); got != "hera" {
		t.Fatalf("active hcom_name match = %q, want hera", got)
	}
	// a closed row's bus name must not vouch, and the stub bus doesn't know it
	if got := resolveBus(path, "ghost", "user", "", "", "/no-such-bus"); got != "" {
		t.Fatalf("closed hcom_name match = %q, want empty", got)
	}
	// literal bus name unknown to the registry validates against the child's bus
	if got := resolveBus(path, "lone-wolf", "user", "", "", "/no-such-bus"); got != "lone-wolf" {
		t.Fatalf("literal bus name = %q, want lone-wolf", got)
	}
	// a name live nowhere still refuses
	if got := resolveBus(path, "nosuch", "user", "", "", "/no-such-bus"); got != "" {
		t.Fatalf("unknown name = %q, want empty", got)
	}
	// literal validation works even with NO readable registry (non-bus-env shell)
	if got := resolveBus(filepath.Join(t.TempDir(), "absent.jsonl"), "lone-wolf", "user", "", "", "/no-such-bus"); got != "lone-wolf" {
		t.Fatalf("literal without registry = %q, want lone-wolf", got)
	}
	// an EXPLICIT but unresolvable --notify-to must not fall through to the
	// spawner's own resolution — a typo must never silently redirect reports
	if got := resolveBus(path, "nosuch", "guid-hera", "p_orch", "term_ORCH", "/no-such-bus"); got != "" {
		t.Fatalf("unresolvable notifyTo fell through to spawner = %q, want empty", got)
	}
}

// TestResolveSpawnerBusReusedPaneTiebreaker pins TASK-035 P1-a: notify
// resolution of a reused pane must NOT silently last-pick a stale row. A stub
// `hcom list --json` reports whichever names STUB_JOINED lists as joined; the
// spawner pane p_reuse holds three active rows (hera/vore/zero).
func TestResolveSpawnerBusReusedPaneTiebreaker(t *testing.T) {
	stubDir := t.TempDir()
	stub := "#!/bin/sh\n" +
		"printf '['\n" +
		"first=1\n" +
		"for n in $STUB_JOINED; do\n" +
		"  [ $first -eq 1 ] || printf ','\n" +
		"  printf '{\"name\":\"%s\",\"tag\":\"x\",\"directory\":\"/d\",\"created_at\":\"2026-01-01T00:00:00Z\"}' \"$n\"\n" +
		"  first=0\n" +
		"done\n" +
		"printf ']\\n'\n"
	if err := os.WriteFile(filepath.Join(stubDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	path := filepath.Join(t.TempDir(), "registry.jsonl")
	for _, row := range []string{
		`{"guid":"guid-hera","label":"hera","pane_id":"p_reuse","terminal_id":"term_reuse","hcom_name":"hera-rive","status":"active"}`,
		`{"guid":"guid-vore","label":"vore","pane_id":"p_reuse","terminal_id":"term_reuse","hcom_name":"vore-lilo","status":"active"}`,
		`{"guid":"guid-zero","label":"zero","pane_id":"p_reuse","terminal_id":"term_reuse","hcom_name":"zero-mano","status":"active"}`,
		`{"guid":"guid-solo","label":"solo","pane_id":"p_solo","terminal_id":"term_solo","hcom_name":"solo-teki","status":"active"}`,
	} {
		if err := registry.Append(path, []byte(row)); err != nil {
			t.Fatal(err)
		}
	}

	// One live (@hera) among stale → notify routes to it, not last-in-guid @zero.
	t.Setenv("STUB_JOINED", "hera-rive")
	if name, amb := resolveSpawnerBus(path, "", "user", "p_reuse", "", "/bus", io.Discard); name != "hera-rive" || amb {
		t.Fatalf("one-live reuse = (%q, %v), want (hera-rive, false)", name, amb)
	}

	// Two live at once → ambiguous, warn + skip (best-effort), never guess.
	t.Setenv("STUB_JOINED", "hera-rive zero-mano")
	var warn bytes.Buffer
	if name, amb := resolveSpawnerBus(path, "", "user", "p_reuse", "", "/bus", &warn); name != "" || !amb {
		t.Fatalf("multi-live reuse = (%q, %v), want (\"\", true)", name, amb)
	}
	for _, want := range []string{"ambiguous", "guid-hera", "guid-zero"} {
		if !bytes.Contains(warn.Bytes(), []byte(want)) {
			t.Errorf("ambiguity warning missing %q; got: %s", want, warn.String())
		}
	}

	// None live → also ambiguous skip (can't tell which owns the pane now).
	t.Setenv("STUB_JOINED", "")
	if name, amb := resolveSpawnerBus(path, "", "user", "p_reuse", "", "/bus", io.Discard); name != "" || !amb {
		t.Fatalf("none-live reuse = (%q, %v), want (\"\", true)", name, amb)
	}

	// Single candidate (p_solo) resolves unchanged — no liveness probe, no skip.
	if name, amb := resolveSpawnerBus(path, "", "user", "p_solo", "", "/bus", io.Discard); name != "solo-teki" || amb {
		t.Fatalf("single-candidate solo = (%q, %v), want (solo-teki, false)", name, amb)
	}
	// Explicit --notify-to <pane-id> for the reused pane runs the same tiebreaker.
	t.Setenv("STUB_JOINED", "vore-lilo")
	if name, amb := resolveSpawnerBus(path, "p_reuse", "user", "", "", "/bus", io.Discard); name != "vore-lilo" || amb {
		t.Fatalf("explicit notify-to reuse = (%q, %v), want (vore-lilo, false)", name, amb)
	}
}

func TestCheckoutForDirWalksUpToCheckoutRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tools", "herder", "shims"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "herder"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "tools", "herder", "internal")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	gotRoot, gotBin, ok := checkoutForDir(nested)
	if !ok || gotRoot != root || gotBin != filepath.Join(root, "bin", "herder") {
		t.Fatalf("checkoutForDir(%q) = (%q, %q, %v), want (%q, ..., true)", nested, gotRoot, gotBin, ok, root)
	}
	if _, _, ok := checkoutForDir(t.TempDir()); ok {
		t.Fatal("checkoutForDir on a plain dir = ok, want miss")
	}
}

func TestRegistryCapturedNameUsesLatestEnrichmentRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if err := registry.Append(path, []byte(`{"guid":"guid-1","short_guid":"guid","label":"worker-guid","hcom_name":"","status":"active"}`)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Append(path, []byte(`{"guid":"guid-1","short_guid":"guid","label":"worker-guid","hcom_name":"worker-rive","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-1","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws","branch":"","ts":"2026-07-03T00:00:00Z"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := registryCapturedName(path, "guid-1"); got != "worker-rive" {
		t.Fatalf("registryCapturedName = %q, want worker-rive", got)
	}
}
