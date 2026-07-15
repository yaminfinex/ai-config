package spawncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/missioncontext"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/shellquote"
)

type cleanupHerdr struct {
	closed bool
	calls  []string
}

type spawnResponse struct {
	want string
	out  []byte
	rc   int
	err  error
}

type scriptedSpawnClient struct {
	responses []spawnResponse
	calls     []string
}

func (c *scriptedSpawnClient) next(args ...string) (spawnResponse, error) {
	call := strings.Join(args, " ")
	c.calls = append(c.calls, call)
	if len(c.responses) == 0 {
		return spawnResponse{}, errors.New("unexpected call")
	}
	r := c.responses[0]
	c.responses = c.responses[1:]
	if call != r.want {
		return spawnResponse{}, fmt.Errorf("call = %q, want %q", call, r.want)
	}
	return r, nil
}

func (c *scriptedSpawnClient) Combined(args ...string) ([]byte, int, error) {
	r, err := c.next(args...)
	if err != nil {
		return nil, 64, err
	}
	return r.out, r.rc, r.err
}

func (c *scriptedSpawnClient) Output(args ...string) ([]byte, error) {
	r, err := c.next(args...)
	if err != nil {
		return nil, err
	}
	return r.out, r.err
}

func (c *scriptedSpawnClient) Run(args ...string) (int, error) {
	r, err := c.next(args...)
	if err != nil {
		return 64, err
	}
	return r.rc, r.err
}

func assertSpawnScriptConsumed(t *testing.T, client *scriptedSpawnClient) {
	t.Helper()
	if len(client.responses) != 0 {
		t.Fatalf("%d scripted response(s) were not consumed; calls = %v", len(client.responses), client.calls)
	}
}

func (f *cleanupHerdr) Combined(args ...string) ([]byte, int, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(args) >= 2 && args[0] == "pane" && args[1] == "get" {
		if f.closed {
			return []byte(`{"error":{"code":"pane_not_found"}}`), 4, nil
		}
		return []byte(`{"result":{"pane":{"pane_id":"p_new","terminal_id":"term_new"}}}`), 0, nil
	}
	if len(args) >= 2 && args[0] == "pane" && args[1] == "close" {
		f.closed = true
		return []byte(`{"result":{"type":"closed"}}`), 0, nil
	}
	return nil, 64, nil
}

func (f *cleanupHerdr) Output(args ...string) ([]byte, error) { return nil, errors.New("unused") }
func (f *cleanupHerdr) Run(args ...string) (int, error)       { return 64, errors.New("unused") }

func TestRegistryRefusalClosesAndConfirmsLaunchedPane(t *testing.T) {
	client := &scriptedSpawnClient{responses: []spawnResponse{
		{want: "pane get p_new", out: []byte(`{"result":{"pane":{"pane_id":"p_new","terminal_id":"term_new"}}}`)},
		{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
		{want: "pane close p_new", out: []byte(`{"result":{"type":"ok"}}`)},
		{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
		{want: "pane get p_new", out: []byte(`{"error":{"code":"pane_not_found"}}`), rc: 1},
	}}
	var stderr strings.Builder
	r := &runner{
		herdr:  client,
		stderr: &stderr,
		updateRegistry: func(string, registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
			return nil, errors.New("lock refused")
		},
	}
	record := spawnRecord{
		GUID:       "guid-new",
		ShortGUID:  "guid-new",
		Label:      "worker-new",
		Role:       "worker",
		Agent:      "bash",
		PaneID:     "p_new",
		TerminalID: "term_new",
		Status:     "active",
		StartedAt:  "2026-07-10T00:00:00Z",
		Mission:    &v2.Mission{Slug: "alpha", Source: missioncontext.SourceExplicit},
	}
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if code := r.registerSpawnOrRollback(path, record); code != 1 {
		t.Fatalf("registerSpawnOrRollback() = %d, want 1", code)
	}
	assertSpawnScriptConsumed(t, client)
	if !strings.Contains(stderr.String(), "registry write refused: lock refused") || !strings.Contains(stderr.String(), "cleanup confirmed") {
		t.Fatalf("stderr = %q, want refusal plus confirmed cleanup", stderr.String())
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("registry path exists after refused write: %v", statErr)
	}
}

func TestParseArgsAcceptsMission(t *testing.T) {
	opts, code := parseArgs([]string{"--role", "worker", "--agent", "bash", "--mission", "alpha"}, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("parseArgs() code = %d, want 0", code)
	}
	if opts.MissionSlug != "alpha" {
		t.Fatalf("MissionSlug = %q, want alpha", opts.MissionSlug)
	}
}

func TestSpawnMissionRefusalStopsBeforePaneCreation(t *testing.T) {
	stubDir := t.TempDir()
	called := filepath.Join(stubDir, "called")
	stub := "#!/bin/sh\ntouch " + shellquote.Quote(called) + "\nexit 0\n"
	for _, name := range []string{"herdr", "jq"} {
		if err := os.WriteFile(filepath.Join(stubDir, name), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("PATH", stubDir)
	missionsRepo := t.TempDir()
	if err := os.Mkdir(filepath.Join(missionsRepo, "missions"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISSIONS_REPO", missionsRepo)

	tests := []struct {
		name       string
		slug       string
		wantCause  string
		wantRemedy string
	}{
		{"invalid slug", "bad--slug", "invalid_mission_slug", "use lowercase letters, digits, and single hyphens, with no trailing hyphen"},
		{"missing mission", "missing", "mission_not_found", "check the slug or create the mission"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.Remove(called); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			var stderr strings.Builder
			code := Run([]string{"--role", "worker", "--agent", "bash", "--mission", tt.slug}, io.Discard, &stderr)
			if code != 1 {
				t.Fatalf("Run() code = %d, want 1", code)
			}
			for _, want := range []string{"refused [" + tt.wantCause + "]", tt.wantRemedy} {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
			if _, err := os.Stat(called); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("herdr or jq executed before mission refusal: %v", err)
			}
		})
	}
}

func TestRegisterSpawnWritesInitialExplicitMission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	record := missionSpawnRecord(&v2.Mission{Slug: "alpha", Source: missioncontext.SourceExplicit})
	if err := (&runner{}).registerSpawn(path, record); err != nil {
		t.Fatal(err)
	}
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	row := registry.V2ByGUID(projection, record.GUID)
	if row == nil || row.Event != "registered" || row.Mission == nil || row.Mission.Slug != "alpha" || row.Mission.Source != missioncontext.SourceExplicit {
		t.Fatalf("spawn row = %+v, want initial registered row with explicit alpha membership", row)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), `"guid":"guid-mission"`) != 1 || strings.Contains(string(raw), `"event":"mission_joined"`) {
		t.Fatalf("registry = %s, want membership on exactly one initial row", raw)
	}
}

func TestRegisterSpawnRejectsInferredMissionSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	record := missionSpawnRecord(&v2.Mission{Slug: "alpha", Source: missioncontext.SourceCWD})
	err := (&runner{}).registerSpawn(path, record)
	if err == nil || !strings.Contains(err.Error(), "invalid durable mission source") {
		t.Fatalf("registerSpawn() error = %v, want durable-source refusal", err)
	}
	projection, loadErr := v2.LoadFile(path, v2.LoadOptions{})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if row := registry.V2ByGUID(projection, record.GUID); row != nil {
		t.Fatalf("refused inferred membership persisted: %+v", row)
	}
}

func TestEnrichmentFailureRetainsRegisteredMission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	var stderr strings.Builder
	writes := 0
	r := &runner{
		stderr: &stderr,
		updateRegistry: func(path string, update registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
			writes++
			if writes == 2 {
				return nil, errors.New("enrichment refused")
			}
			return registry.UpdateLocked(path, update)
		},
	}
	record := missionSpawnRecord(&v2.Mission{Slug: "alpha", Source: missioncontext.SourceExplicit})
	if err := r.registerSpawn(path, record); err != nil {
		t.Fatal(err)
	}
	if code := r.persistCapturedHcomName(path, record.GUID, "worker-rive"); code != 1 {
		t.Fatalf("persistCapturedHcomName() = %d, want nonzero enrichment failure", code)
	}
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	row := registry.V2ByGUID(projection, record.GUID)
	if row == nil || row.State != v2.StateSeated || row.Mission == nil || row.Mission.Slug != "alpha" || row.Mission.Source != missioncontext.SourceExplicit {
		t.Fatalf("registered row after enrichment failure = %+v, want seated explicit alpha membership", row)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), `"guid":"guid-mission"`) != 1 || strings.Contains(string(raw), `"event":"mission_left"`) {
		t.Fatalf("registry after enrichment failure = %s, want unchanged registered membership", raw)
	}
	if !strings.Contains(stderr.String(), "enrichment refused") {
		t.Fatalf("stderr = %q, want enrichment failure", stderr.String())
	}
}

func TestSpawnMissionSurvivesRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	record := missionSpawnRecord(&v2.Mission{Slug: "alpha", Source: missioncontext.SourceExplicit})
	if err := (&runner{}).registerSpawn(path, record); err != nil {
		t.Fatal(err)
	}
	beforeNoise, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
			current := registry.V2ByGUID(tx.Projection, record.GUID)
			if current == nil {
				t.Fatal("spawn row missing before rotation")
			}
			next := *current
			next.Event = "recognised"
			next.RecordedAt = ""
			next.Mission = nil
			return []v2.SessionRecord{next}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := registry.SingleOutcome(outcomes)
		if err != nil || outcome.Err() != nil {
			t.Fatalf("successor outcome = %+v, err = %v", outcome, err)
		}
	}
	t.Setenv("HERDER_REGISTRY_ROTATE_BYTES", strconv.Itoa(len(beforeNoise)+512))
	if _, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	after, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	row := registry.V2ByGUID(after, record.GUID)
	if row == nil || row.Mission == nil || row.Mission.Slug != "alpha" || row.Mission.Source != missioncontext.SourceExplicit {
		t.Fatalf("spawn membership after rotation = %+v", row)
	}
	archives, err := filepath.Glob(filepath.Join(dir, "registry.jsonl.archive", "*-rotation.jsonl"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("rotation archives = %v, err = %v, want one", archives, err)
	}
}

func missionSpawnRecord(mission *v2.Mission) spawnRecord {
	return spawnRecord{
		GUID:       "guid-mission",
		ShortGUID:  "guid-mis",
		Label:      "worker-mission",
		Role:       "worker",
		Agent:      "bash",
		PaneID:     "p_mission",
		TerminalID: "term_mission",
		CWD:        "/repo",
		Status:     "active",
		StartedAt:  "2026-07-15T00:00:00Z",
		Mission:    mission,
	}
}

func TestSeedPaneClosePreservesFocus(t *testing.T) {
	client := &scriptedSpawnClient{responses: []spawnResponse{
		{want: "pane get p_seed", out: []byte(`{"result":{"pane":{"pane_id":"p_seed","terminal_id":"term_seed"}}}`)},
		{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
		{want: "pane close p_seed", out: []byte(`{"result":{"type":"ok"}}`)},
		{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
	}}
	var stderr strings.Builder
	r := &runner{herdr: client, stderr: &stderr}
	if !r.closeSeedPane("p_seed", "term_seed", "term_agent") {
		t.Fatalf("closeSeedPane returned false; stderr = %q", stderr.String())
	}
	assertSpawnScriptConsumed(t, client)
}

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

func TestSpawnLabelPrefixReplacement(t *testing.T) {
	if got := spawnLabel("spec", "", "abcd1234"); got != "spec-abcd1234" {
		t.Fatalf("default label = %q, want spec-abcd1234", got)
	}
	if got := spawnLabel("spec", "spec-hera", "abcd1234"); got != "spec-hera-abcd1234" {
		t.Fatalf("prefix label = %q, want spec-hera-abcd1234", got)
	}
}

func TestNewTabMoveArgsCarryResolvedFocus(t *testing.T) {
	for _, tc := range []struct {
		name      string
		focusArgs []string
		wantFocus string
	}{
		{name: "default", wantFocus: "--no-focus"},
		{name: "explicit focus", focusArgs: []string{"--focus"}, wantFocus: "--focus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--role", "worker", "--agent", "codex", "--new-tab"}, tc.focusArgs...)
			opts, code := parseArgs(args, io.Discard, io.Discard)
			if code != 0 {
				t.Fatalf("parseArgs() code = %d, want 0", code)
			}
			got := strings.Join(newTabMoveArgs("pane-1", "worker", opts.FocusFlag), " ")
			want := "pane move pane-1 --new-tab " + tc.wantFocus + " --label worker"
			if got != want {
				t.Fatalf("move args = %q, want %q", got, want)
			}
		})
	}
}

func TestResolveSpawnerBusMatchesEnrolledPane(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	rows := []string{
		// enrolled orchestrator: pane/terminal identity, bus name, NO spawner guid in play
		`{"guid":"guid-hera","short_guid":"guid-her","label":"orchestrator","pane_id":"p_orch","terminal_id":"term_ORCH","hcom_name":"hera","status":"active"}`,
		// retired session holding the SAME pane id from an older legacy row must not win
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
	// retired sessions never resolve by pane coordinates
	if got := resolveBus(path, "", "user", "p_stale", "", ""); got != "" {
		t.Fatalf("retired pane match = %q, want empty", got)
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

	// --notify-to may BE a bus name: a seated session's hcom_name matches (TASK-023)
	if got := resolveBus(path, "hera", "user", "", "", "/no-such-bus"); got != "hera" {
		t.Fatalf("seated hcom_name match = %q, want hera", got)
	}
	// a retired session's bus name must not vouch, and the stub bus doesn't know it
	if got := resolveBus(path, "ghost", "user", "", "", "/no-such-bus"); got != "" {
		t.Fatalf("retired hcom_name match = %q, want empty", got)
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
// spawner pane p_reuse holds three seated sessions (hera/vore/zero).
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

// TestResendCommandForOnlySafeResults pins the recovery-command contract
// (TASK-036): the exact `herder send` resend command is surfaced ONLY for the
// delivery results where a resend is provably safe (nothing went on the wire) —
// bind_timeout and ready_match_timeout — and is empty for every other result so
// the omitempty JSON field stays absent where a resend is NOT the remedy.
func TestResendCommandForOnlySafeResults(t *testing.T) {
	const label = "worker-abc1234"
	safe := map[string]bool{"bind_timeout": true, "ready_match_timeout": true}
	for _, result := range []string{
		"bind_timeout", "ready_match_timeout",
		"delivered", "queued", "blocked_trust_modal", "send_failed", "not_attempted",
	} {
		got := resendCommandFor(result, label, "do the thing")
		if safe[result] {
			want := resendCommand(label, "do the thing")
			if got != want {
				t.Fatalf("resendCommandFor(%q) = %q, want %q", result, got, want)
			}
		} else if got != "" {
			t.Fatalf("resendCommandFor(%q) = %q, want empty (resend is not the remedy)", result, got)
		}
	}
}

// TestResendCommandQuotesPromptVerbatim confirms the prompt round-trips through
// shell quoting so a copy-paste resend re-sends the exact bytes — including a
// multi-line brief with the notify appendix already folded in.
func TestResendCommandQuotesPromptVerbatim(t *testing.T) {
	prompt := "review unit X\nread the plan, then the diff\nreport 'findings'"
	cmd := resendCommand("x-review-9f8e", prompt)
	const prefix = "herder send x-review-9f8e "
	if len(cmd) <= len(prefix) || cmd[:len(prefix)] != prefix {
		t.Fatalf("resendCommand = %q, want prefix %q", cmd, prefix)
	}
	// A newline-bearing prompt must be quoted (never emitted raw), so the command
	// stays a single shell word that pastes back intact.
	quoted := cmd[len(prefix):]
	for _, r := range quoted {
		if r == '\n' {
			t.Fatalf("resendCommand left a raw newline in the quoted prompt: %q", cmd)
		}
	}
}

// TestResendCommandQuotesMetacharLabel pins that the LABEL is quoted too: labels
// are built from --label-prefix verbatim and metachar prefixes are accepted, so
// an unquoted label would split/expand/glob when the recovery command is pasted.
func TestResendCommandQuotesMetacharLabel(t *testing.T) {
	label := `quote;$` + "`" + `&<>("* )-abc1234`
	cmd := resendCommand(label, "do the thing")
	want := "herder send " + shellquote.Quote(label) + " " + shellquote.Quote("do the thing")
	if cmd != want {
		t.Fatalf("resendCommand = %q, want %q", cmd, want)
	}
	// The raw metachar label must NOT appear verbatim — proof it was quoted, not
	// pasted through as splittable/expandable shell.
	if strings.Contains(cmd, "herder send "+label) {
		t.Fatalf("resendCommand emitted the label RAW: %q", cmd)
	}
}
