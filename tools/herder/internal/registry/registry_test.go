package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

func writeRegistry(t *testing.T, rows ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func guids(recs []Record) []string {
	var out []string
	for _, r := range recs {
		if r.GUID == nil {
			out = append(out, "<null>")
		} else {
			out = append(out, *r.GUID)
		}
	}
	return out
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestLoadParsesRowsAndKeepsRaw(t *testing.T) {
	row := `{"guid":"g-1","short_guid":"s1","label":"alpha","role":"worker","agent":"claude","terminal_id":"term_A","pane_id":"p_1","team":"blue","hcom_dir":"/x/.hcom","hcom_name":"alpha-rive","hcom_tag":"worker","status":"active"}`
	path := writeRegistry(t, row)
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	r := recs[0]
	if *r.GUID != "g-1" || *r.ShortGUID != "s1" || *r.Label != "alpha" ||
		r.Role != "worker" || r.Agent != "claude" || r.TerminalID != "term_A" ||
		r.PaneID != "p_1" || r.Team != "blue" || r.HcomDir != "/x/.hcom" ||
		r.HcomName != "alpha-rive" || r.HcomTag != "worker" || r.Status != "active" {
		t.Errorf("typed fields wrong: %+v", r)
	}
	if string(r.Raw) != row {
		t.Errorf("Raw = %s, want original row", r.Raw)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.jsonl"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestLoadToleratesBlankLinesAndLongRows(t *testing.T) {
	// json.Decoder streams values like `jq -s`, so blank lines are noise and
	// a row larger than any line-scanner default (spawn records embed full
	// prompts in argv) must still parse.
	big := `{"guid":"g-big","argv":["` + strings.Repeat("x", 200_000) + `"],"status":"active"}`
	path := writeRegistry(t, `{"guid":"g-1","status":"active"}`, "", big, "")
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
}

func TestLoadQuarantinesMalformedRows(t *testing.T) {
	path := writeRegistry(t, `{"guid":"g-1"}`, `{not json`)
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || ptrString(recs[0].GUID) != "g-1" {
		t.Fatalf("recs = %+v, want only valid row", recs)
	}
	path = writeRegistry(t, `"just a string"`)
	recs, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("recs = %+v, want non-object row quarantined", recs)
	}
}

func TestLatestByGUIDCollapsesAndSorts(t *testing.T) {
	path := writeRegistry(t,
		`{"guid":"g-c","status":"active"}`,
		`{"guid":"g-a","status":"active"}`,
		`{"status":"no-guid-1"}`,
		`{"guid":"g-a","status":"closed"}`,
		`{"guid":null,"status":"no-guid-2"}`,
		`{"guid":"g-b","status":"active"}`,
	)
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := LatestByGUID(recs)
	want := []string{"<null>", "g-a", "g-b", "g-c"}
	if g := guids(got); strings.Join(g, ",") != strings.Join(want, ",") {
		t.Fatalf("collapsed guid order = %v, want %v", g, want)
	}
	// Missing guid and explicit null group together; the LAST file row wins.
	if got[0].Status != "no-guid-2" {
		t.Errorf("null-guid group kept %q, want last row no-guid-2", got[0].Status)
	}
	if got[1].Status != "closed" {
		t.Errorf("g-a group kept %q, want last row closed", got[1].Status)
	}
}

func TestResolve(t *testing.T) {
	path := writeRegistry(t,
		`{"guid":"guid-alpha-0000","short_guid":"alpha","label":"alpha-l","status":"active"}`,
		`{"guid":"guid-beta-0000","short_guid":"beta","label":"beta-l","status":"active"}`,
		`{"guid":"guid-beta-0000","short_guid":"beta","label":"beta-l","status":"closed"}`,
	)
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"guid-beta-0000", "beta", "beta-l"} {
		hit := Resolve(recs, target)
		if hit == nil {
			t.Fatalf("Resolve(%q) = nil, want the beta record", target)
		}
		if hit.Status != "closed" {
			t.Errorf("Resolve(%q).Status = %q, want closed (latest row)", target, hit.Status)
		}
	}
	if hit := Resolve(recs, "term_XYZ"); hit != nil {
		t.Errorf("Resolve(term_XYZ) = %+v, want nil (herdr-verbatim path)", hit)
	}
	// jq: null == "" is false — a record without a label never matches "".
	if hit := Resolve([]Record{{}}, ""); hit != nil {
		t.Errorf("Resolve(\"\") on fieldless record = %+v, want nil", hit)
	}
}

func TestActiveCandidatesByPaneOrTerminal(t *testing.T) {
	// A reused pane p_1 carries three manual identities: two stale-but-active
	// (bus_a already superseded on the bus, bus_b likewise) and one live.
	// gamma sits on a different pane; delta is closed on p_1. Candidates must
	// be every ACTIVE row on the coordinate, in guid order, and nothing else.
	path := writeRegistry(t,
		`{"guid":"guid-alpha-0000","short_guid":"alpha","label":"a","pane_id":"p_1","terminal_id":"term_1","hcom_name":"bus_a","status":"active"}`,
		`{"guid":"guid-beta-0000","short_guid":"beta","label":"b","pane_id":"p_1","terminal_id":"term_1","hcom_name":"bus_b","status":"active"}`,
		`{"guid":"guid-gamma-0000","short_guid":"gamma","label":"g","pane_id":"p_2","terminal_id":"term_2","hcom_name":"bus_g","status":"active"}`,
		`{"guid":"guid-delta-0000","short_guid":"delta","label":"d","pane_id":"p_1","terminal_id":"term_1","hcom_name":"bus_d","status":"closed"}`,
	)
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"p_1", "term_1"} {
		got := ActiveCandidatesByPaneOrTerminal(recs, key)
		if len(got) != 2 {
			t.Fatalf("ActiveCandidatesByPaneOrTerminal(%q) = %d rows, want 2 (alpha,beta)", key, len(got))
		}
		if ptrString(got[0].GUID) != "guid-alpha-0000" || ptrString(got[1].GUID) != "guid-beta-0000" {
			t.Errorf("candidates(%q) order = %q,%q, want alpha,beta (guid order)", key, ptrString(got[0].GUID), ptrString(got[1].GUID))
		}
	}
	if got := ActiveCandidatesByPaneOrTerminal(recs, "p_2"); len(got) != 1 || ptrString(got[0].GUID) != "guid-gamma-0000" {
		t.Errorf("candidates(p_2) = %+v, want just gamma", got)
	}
	if got := ActiveCandidatesByPaneOrTerminal(recs, ""); got != nil {
		t.Errorf("candidates(\"\") = %+v, want nil", got)
	}
	if got := ActiveCandidatesByPaneOrTerminal(recs, "p_nope"); got != nil {
		t.Errorf("candidates(unknown) = %+v, want nil", got)
	}
}

func TestActiveLabelOwnerUsesLatestActiveRows(t *testing.T) {
	path := writeRegistry(t,
		`{"guid":"guid-alpha-0000","short_guid":"alpha","label":"shared","status":"active"}`,
		`{"guid":"guid-beta-0000","short_guid":"beta","label":"closed-label","status":"active"}`,
		`{"guid":"guid-beta-0000","short_guid":"beta","label":"closed-label","status":"closed"}`,
		`{"guid":"guid-gamma-0000","short_guid":"gamma","label":"shared","status":"active"}`,
	)
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := ActiveLabelOwner(recs, "shared", "guid-alpha-0000")
	if owner == nil || ptrString(owner.GUID) != "guid-gamma-0000" {
		t.Fatalf("ActiveLabelOwner(shared, except alpha) = %+v, want gamma", owner)
	}
	if owner := ActiveLabelOwner(recs, "shared", "guid-gamma-0000"); owner == nil || ptrString(owner.GUID) != "guid-alpha-0000" {
		t.Fatalf("ActiveLabelOwner(shared, except gamma) = %+v, want alpha", owner)
	}
	if owner := ActiveLabelOwner(recs, "closed-label", ""); owner != nil {
		t.Fatalf("closed latest row owns label: %+v, want nil", owner)
	}
	if owner := ActiveLabelOwner(recs, "", ""); owner != nil {
		t.Fatalf("empty label owner = %+v, want nil", owner)
	}
}

func TestResolveByToolSessionIDScansClosedAndOlderRows(t *testing.T) {
	path := writeRegistry(t,
		`{"guid":"guid-alpha-0000","short_guid":"alpha","label":"alpha-old","status":"active","provenance":{"mechanism":"spawn","tool_session_id":"sess-alpha","tag":"worker"}}`,
		`{"guid":"guid-alpha-0000","short_guid":"alpha","label":"alpha-latest","status":"closed","provenance":{"mechanism":"spawn","tool_session_id":"","tag":"worker"}}`,
		`{"guid":"guid-beta-0000","short_guid":"beta","label":"beta","status":"active","provenance":{"mechanism":"spawn","tool_session_id":"sess-beta","tag":"worker"}}`,
	)
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	hit := ResolveByToolSessionID(recs, "sess-alpha")
	if hit == nil {
		t.Fatal("ResolveByToolSessionID returned nil")
	}
	if ptrString(hit.Label) != "alpha-latest" || hit.Status != "closed" {
		t.Fatalf("hit = label %q status %q, want latest closed alpha row", ptrString(hit.Label), hit.Status)
	}
	if got := ToolSessionIDForGUID(recs, "guid-alpha-0000"); got != "sess-alpha" {
		t.Fatalf("ToolSessionIDForGUID = %q, want sess-alpha", got)
	}
	prov := PreserveToolSessionID(Provenance{Mechanism: "spawn"}, recs, "guid-alpha-0000")
	if prov.ToolSessionID != "sess-alpha" {
		t.Fatalf("PreserveToolSessionID = %q, want sess-alpha", prov.ToolSessionID)
	}
	if hit := ResolveByToolSessionID(recs, ""); hit != nil {
		t.Fatalf("empty session resolved %+v, want nil", hit)
	}
}

func TestAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "registry.jsonl")
	if err := Append(path, []byte(`{"guid":"g-1","status":"active"}`)); err != nil {
		t.Fatal(err)
	}
	// Rows arriving with a trailing newline must not double it.
	if err := Append(path, []byte(`{"guid":"g-2","status":"active"}`+"\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2: %s", len(lines), data)
	}
	for i, line := range lines {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row["kind"] != "session" || row["state"] != "seated" || row["status"] != nil {
			t.Fatalf("line %d = %s, want clean seated v2 session row", i+1, line)
		}
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || ptrString(recs[0].GUID) != "g-1" || recs[0].Status != "active" || ptrString(recs[1].GUID) != "g-2" {
		t.Fatalf("legacy view = %+v, want active g-1/g-2 records", recs)
	}
}

func TestLoadDerivesLegacyViewFromV2Rows(t *testing.T) {
	path := writeRegistry(t,
		`{"kind":"session","guid":"guid-seated","event":"registered","recorded_at":"2026-07-08T00:00:00Z","state":"seated","label":"seat","role":"worker","tool":"codex","seat":{"kind":"herdr","terminal_id":"term_1","pane_id":"p_1","hcom_name":"bus-seat","namespace":"/hcom"},"provenance":{"mechanism":"spawn","tool_session_id":"sess-1","tag":"worker"}}`,
		`{"kind":"session","guid":"guid-unseated","event":"unseated","recorded_at":"2026-07-08T00:00:01Z","state":"unseated","label":"dormant","role":"worker","tool":"claude","provenance":{"mechanism":"spawn","tag":"worker"}}`,
		`{"kind":"session","guid":"guid-retired","event":"retired","recorded_at":"2026-07-08T00:00:02Z","state":"retired","label":"done","tool":"bash"}`,
	)
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	byGUID := map[string]Record{}
	for _, rec := range recs {
		byGUID[ptrString(rec.GUID)] = rec
	}
	if got := byGUID["guid-seated"]; got.Status != "active" || got.PaneID != "p_1" || got.TerminalID != "term_1" || got.HcomName != "bus-seat" || got.HcomDir != "/hcom" || got.Agent != "codex" {
		t.Fatalf("seated legacy view = %+v", got)
	}
	if got := byGUID["guid-unseated"]; got.Status != "active" || got.PaneID != "" || got.Agent != "claude" {
		t.Fatalf("unseated legacy view = %+v, want active status without seat", got)
	}
	if got := byGUID["guid-retired"]; got.Status != "closed" || got.Agent != "bash" {
		t.Fatalf("retired legacy view = %+v, want closed bash", got)
	}
}

func TestTwoProcessLabelClaimsOneWinner(t *testing.T) {
	if os.Getenv("HERDER_REGISTRY_CLAIM_HELPER") == "1" {
		runLabelClaimHelper()
		return
	}
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	start := filepath.Join(t.TempDir(), "start")
	cmds := make([]*exec.Cmd, 0, 2)
	stderrs := make([]*bytes.Buffer, 0, 2)
	for _, guid := range []string{"guid-alpha", "guid-beta"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestTwoProcessLabelClaimsOneWinner$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			"HERDER_REGISTRY_CLAIM_HELPER=1",
			"HERDER_REGISTRY_CLAIM_PATH="+path,
			"HERDER_REGISTRY_CLAIM_GUID="+guid,
			"HERDER_REGISTRY_CLAIM_START="+start,
		)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cmds = append(cmds, cmd)
		stderrs = append(stderrs, &stderr)
		if err := cmd.Start(); err != nil {
			t.Fatalf("start helper %s: %v", guid, err)
		}
		t.Cleanup(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
	}
	winners := 0
	losers := 0
	if err := os.WriteFile(start, []byte("go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range cmds {
		err := cmd.Wait()
		if err == nil {
			winners++
			continue
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("unexpected helper error: %v", err)
		}
		stderr := stderrs[i].String()
		if exitErr.ExitCode() == 42 && strings.Contains(stderr, `label "shared" already belongs`) {
			losers++
			continue
		}
		t.Fatalf("unexpected helper exit %d stderr=%s", exitErr.ExitCode(), stderr)
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("winners=%d losers=%d, want one winner and one loser", winners, losers)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if owner := ActiveLabelOwner(recs, "shared", ""); owner == nil {
		t.Fatal("legacy resolver sees no shared owner")
	}
}

func runLabelClaimHelper() {
	start := os.Getenv("HERDER_REGISTRY_CLAIM_START")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(start); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Stderr.WriteString("timed out waiting for start barrier\n")
			os.Exit(2)
		}
		time.Sleep(10 * time.Millisecond)
	}
	path := os.Getenv("HERDER_REGISTRY_CLAIM_PATH")
	guid := os.Getenv("HERDER_REGISTRY_CLAIM_GUID")
	label := "shared"
	prov := Provenance{Mechanism: "spawn"}
	rec := Record{GUID: &guid, Label: &label, Status: "active", Provenance: &prov}
	_, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{V2FromRecord(rec, "registered", v2.StateSeated, "")}, nil
	})
	if err == nil {
		os.Exit(0)
	}
	os.Stderr.WriteString(err.Error() + "\n")
	if strings.Contains(err.Error(), `label "shared" already belongs`) {
		os.Exit(42)
	}
	os.Exit(2)
}

func TestLockedValidatorCarriesLegacySeatOnRename(t *testing.T) {
	path := writeRegistry(t, `{"guid":"guid-legacy","short_guid":"legacy","label":"old","role":"worker","agent":"codex","terminal_id":"term_OLD","pane_id":"p_old","hcom_dir":"/hcom","hcom_name":"bus-old","status":"active"}`)
	if _, err := UpdateLocked(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		current := V2ByGUID(tx.Projection, "guid-legacy")
		if current == nil {
			t.Fatal("missing legacy projection row")
		}
		next := *current
		next.Event = "labelled"
		next.Label = "new"
		return []v2.SessionRecord{next}, nil
	}); err != nil {
		t.Fatal(err)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	latest := Resolve(recs, "guid-legacy")
	if latest == nil || ptrString(latest.Label) != "new" || latest.PaneID != "p_old" || latest.TerminalID != "term_OLD" || latest.HcomName != "bus-old" || latest.HcomDir != "/hcom" {
		t.Fatalf("latest = %+v, want renamed row with legacy seat carried", latest)
	}
}

func TestRegisteredCarriesRecognisedHcomName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	guid, label := "guid-carry", "worker"
	recognised := Record{
		GUID:       &guid,
		Label:      &label,
		Role:       "worker",
		Agent:      "codex",
		PaneID:     "p_sidecar",
		TerminalID: "term_sidecar",
		HcomName:   "worker-rive",
		HcomDir:    "/hcom",
		Status:     "active",
	}
	if err := AppendLegacySessionEvent(path, mustMarshalRecord(t, recognised), "recognised", v2.StateSeated); err != nil {
		t.Fatal(err)
	}
	registered := Record{
		GUID:       &guid,
		Label:      &label,
		Role:       "worker",
		Agent:      "codex",
		PaneID:     "p_spawn",
		TerminalID: "term_spawn",
		HcomDir:    "/hcom",
		Status:     "active",
		Provenance: &Provenance{Mechanism: "spawn", ToolSessionID: "sess-spawn", Tag: "worker"},
	}
	if _, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{V2FromRecord(registered, "registered", v2.StateSeated, "2026-07-08T00:00:01Z")}, nil
	}); err != nil {
		t.Fatal(err)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	latest := Resolve(recs, guid)
	if latest == nil || latest.HcomName != "worker-rive" || latest.PaneID != "p_spawn" || latest.TerminalID != "term_spawn" {
		t.Fatalf("latest legacy view = %+v, want registered seat with carried hcom_name and fresh spawn coordinates", latest)
	}
	collapsed := LatestByGUID(recs)
	if len(collapsed) != 1 || collapsed[0].HcomName != "worker-rive" {
		t.Fatalf("LatestByGUID = %+v, want bus-reachable hcom_name", collapsed)
	}
}

func TestRegisteredCarrySurvivesRotationReseed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	guid, label := "guid-reseed", "worker"
	recognised := Record{
		GUID:       &guid,
		Label:      &label,
		Role:       "worker",
		Agent:      "codex",
		PaneID:     "p_sidecar",
		TerminalID: "term_sidecar",
		HcomName:   "worker-rive",
		HcomDir:    "/hcom",
		Status:     "active",
	}
	if err := AppendLegacySessionEvent(path, mustMarshalRecord(t, recognised), "recognised", v2.StateSeated); err != nil {
		t.Fatal(err)
	}
	registered := Record{
		GUID:       &guid,
		Label:      &label,
		Role:       "worker",
		Agent:      "codex",
		PaneID:     "p_spawn",
		TerminalID: "term_spawn",
		HcomDir:    "/hcom",
		Status:     "active",
	}
	if _, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{V2FromRecord(registered, "registered", v2.StateSeated, "2026-07-08T00:00:01Z")}, nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := v2.Load(bytes.NewReader(data), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reseedPath := filepath.Join(t.TempDir(), "registry.jsonl")
	for _, rec := range proj.Sessions() {
		if rec.State == v2.StateRetired || rec.State == v2.StateLost {
			continue
		}
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := Append(reseedPath, b); err != nil {
			t.Fatal(err)
		}
	}
	reseeded, err := Load(reseedPath)
	if err != nil {
		t.Fatal(err)
	}
	latest := Resolve(reseeded, guid)
	if latest == nil || latest.HcomName != "worker-rive" || latest.PaneID != "p_spawn" || latest.TerminalID != "term_spawn" {
		t.Fatalf("reseeded latest = %+v, want self-contained registered row with hcom_name", latest)
	}
}

func TestRegisteredNoOpDoesNotAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	guid, label := "guid-noop", "worker"
	row := v2.SessionRecord{
		Kind:       v2.KindSession,
		GUID:       guid,
		Event:      "registered",
		RecordedAt: "2026-07-08T00:00:00Z",
		State:      v2.StateSeated,
		Label:      label,
		Role:       "worker",
		Tool:       "codex",
		Seat: &v2.Seat{
			Kind:        "herdr",
			TerminalID:  "term_spawn",
			PaneID:      "p_spawn",
			HcomName:    "worker-rive",
			Namespace:   "/hcom",
			ConfirmedAt: "2026-07-08T00:00:00Z",
		},
		Continuity: "assumed",
		Lineage:    v2.Lineage{},
		Provenance: v2.Provenance{Mechanism: "spawn", Tag: "worker"},
	}
	if _, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{row}, nil
	}); err != nil {
		t.Fatal(err)
	}
	before := registryRowCount(t, path)
	repeat := row
	repeat.RecordedAt = "2026-07-08T00:00:01Z"
	repeat.Seat = cloneSeat(row.Seat)
	repeat.Seat.HcomName = ""
	if _, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{repeat}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if after := registryRowCount(t, path); after != before {
		t.Fatalf("row count = %d, want unchanged %d after no-op registered append", after, before)
	}
}

func TestAppendLegacyRetiredPreservesCloseReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	row := []byte(`{"guid":"guid-launch","short_guid":"launch","label":"launch","role":"worker","agent":"codex","terminal_id":"term_L","pane_id":"p_l","hcom_dir":"/hcom","hcom_name":"launch-bus","status":"closed","close_result":"launch_failed","close_reason":"pane exited before lifecycle bind"}`)
	if err := AppendLegacySessionEvent(path, row, "retired", v2.StateRetired); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &got); err != nil {
		t.Fatal(err)
	}
	if got["state"] != v2.StateRetired || got["event"] != "retired" || got["close_result"] != "launch_failed" || got["close_reason"] != "pane exited before lifecycle bind" {
		t.Fatalf("row = %s, want retired launch_failed with close_reason", data)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Status != "closed" || recs[0].CloseResult != "launch_failed" || recs[0].CloseReason == "" {
		t.Fatalf("legacy view = %+v, want closed launch_failed", recs)
	}
}

func TestLockedValidatorPreservesRenameAgainstStaleEnrichment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	guid, oldLabel := "guid-stale", "old"
	if err := Append(path, []byte(`{"guid":"`+guid+`","label":"`+oldLabel+`","role":"worker","agent":"codex","pane_id":"p_old","terminal_id":"term_old","status":"active"}`)); err != nil {
		t.Fatal(err)
	}
	stale := Record{GUID: &guid, Label: &oldLabel, Role: "worker", Agent: "codex", PaneID: "p_new", TerminalID: "term_new", HcomName: "bus-new", Status: "active"}
	if _, err := UpdateLocked(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		current := V2ByGUID(tx.Projection, guid)
		next := *current
		next.Event = "labelled"
		next.Label = "new"
		return []v2.SessionRecord{next}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendLegacySessionEvent(path, mustMarshalRecord(t, stale), "recognised", v2.StateSeated); err != nil {
		t.Fatal(err)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	latest := Resolve(recs, guid)
	if latest == nil || ptrString(latest.Label) != "new" || latest.HcomName != "bus-new" {
		t.Fatalf("latest = %+v, want label new with enriched bus", latest)
	}
}

func TestLockedValidatorDoesNotResurrectUnseatedSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	guid, label := "guid-culled", "worker"
	if err := Append(path, []byte(`{"guid":"`+guid+`","label":"`+label+`","role":"worker","agent":"codex","pane_id":"p_old","terminal_id":"term_old","status":"active"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateLocked(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		current := V2ByGUID(tx.Projection, guid)
		next := *current
		next.Event = "unseated"
		next.State = v2.StateUnseated
		next.Seat = nil
		return []v2.SessionRecord{next}, nil
	}); err != nil {
		t.Fatal(err)
	}
	stale := Record{GUID: &guid, Label: &label, Role: "worker", Agent: "codex", PaneID: "p_new", TerminalID: "term_new", HcomName: "bus-new", Status: "active"}
	if err := AppendLegacySessionEvent(path, mustMarshalRecord(t, stale), "recognised", v2.StateSeated); err != nil {
		t.Fatal(err)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	latest := Resolve(recs, guid)
	if latest == nil || latest.PaneID != "" || latest.HcomName != "" {
		t.Fatalf("latest = %+v, want still unseated/no seat after stale heartbeat", latest)
	}
}

func TestLockedWriterRefusesUnlocked(t *testing.T) {
	t.Setenv("HERDER_TEST_FLOCK_REFUSE", "1")
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	err := Append(path, []byte(`{"guid":"g-1","status":"active"}`))
	if err == nil || !strings.Contains(err.Error(), "refusing to write unlocked") {
		t.Fatalf("err = %v, want refusing to write unlocked", err)
	}
}

func mustMarshalRecord(t *testing.T, rec Record) []byte {
	t.Helper()
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func registryRowCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return 0
	}
	return bytes.Count(trimmed, []byte("\n")) + 1
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("HERDER_STATE_DIR", "/custom/state")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	if got := DefaultPath(); got != "/custom/state/registry.jsonl" {
		t.Errorf("HERDER_STATE_DIR precedence: got %q", got)
	}
	t.Setenv("HERDER_STATE_DIR", "")
	if got := DefaultPath(); got != "/xdg/state/herder/registry.jsonl" {
		t.Errorf("XDG_STATE_HOME fallback: got %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	home, _ := os.UserHomeDir()
	if got := DefaultPath(); got != filepath.Join(home, ".local", "state", "herder", "registry.jsonl") {
		t.Errorf("HOME fallback: got %q", got)
	}
}

// jqParityRows exercises the edges the collapse contract hangs on: duplicate
// guids (append order), unsorted guids, a missing guid, an explicit null
// guid, an empty-string guid, and non-ASCII vs ASCII codepoint ordering.
var jqParityRows = []string{
	`{"guid":"g-zulu","short_guid":"zulu","label":"z","status":"active"}`,
	`{"guid":"g-alpha","short_guid":"alpha","label":"a","status":"active"}`,
	`{"status":"missing-guid"}`,
	`{"guid":"g-alpha","short_guid":"alpha","label":"a","status":"closed"}`,
	`{"guid":null,"status":"null-guid"}`,
	`{"guid":"","short_guid":"empty","label":"e","status":"active"}`,
	`{"guid":"g-Ω","short_guid":"omega","label":"o","status":"active"}`,
	`{"guid":"g-alpha","short_guid":"alpha","label":"a","status":"reopened"}`,
}

// TestJQParityCollapse pins LatestByGUID against the real jq program the
// bash scripts run. The goldens were generated through jq, so jq — not our
// reading of its manual — is the spec.
func TestJQParityCollapse(t *testing.T) {
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq not on PATH")
	}
	path := writeRegistry(t, jqParityRows...)

	cmd := exec.Command(jq, "-c", "-s", "group_by(.guid) | map(.[-1]) | .[]", path)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var goRows []string
	for _, rec := range LatestByGUID(recs) {
		goRows = append(goRows, string(rec.Raw))
	}
	jqRows := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if strings.Join(goRows, "\n") != strings.Join(jqRows, "\n") {
		t.Errorf("collapse diverges from jq:\n  go: %v\n  jq: %v", goRows, jqRows)
	}
}

// TestJQParityResolve pins Resolve against the shared bash lookup
// (_registry_record_for / resolve_pane) for every interesting target.
func TestJQParityResolve(t *testing.T) {
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq not on PATH")
	}
	path := writeRegistry(t, jqParityRows...)
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	prog := `group_by(.guid) | map(.[-1])
	  | map(select(.guid==$v or .short_guid==$v or .label==$v))
	  | last // empty`
	targets := []string{"g-alpha", "alpha", "a", "zulu", "", "g-Ω", "term_nope", "missing-guid"}
	for _, target := range targets {
		out, err := exec.Command(jq, "-c", "-s", "--arg", "v", target, prog, path).Output()
		if err != nil {
			t.Fatal(err)
		}
		jqHit := strings.TrimRight(string(out), "\n")

		var goHit string
		if rec := Resolve(recs, target); rec != nil {
			goHit = string(rec.Raw)
		}
		if goHit != jqHit {
			t.Errorf("Resolve(%q) diverges from jq:\n  go: %s\n  jq: %s", target, goHit, jqHit)
		}
	}
}

// The raw rows Load preserves must be exactly what jq -c would emit for the
// same rows (writers use jq -nc, so the file is already jq-compact); parity
// tests above compare Raw against jq output byte-for-byte, which only means
// something if this holds.
func TestRawRowsAreJQCompact(t *testing.T) {
	for _, row := range jqParityRows {
		var v any
		if err := json.Unmarshal([]byte(row), &v); err != nil {
			t.Fatalf("fixture row %q: %v", row, err)
		}
	}
}

func TestBuildProvenanceSpawnedBy(t *testing.T) {
	// Ambient env of a session that was ITSELF spawned: HERDER_SPAWNED_BY names
	// that session's own spawner (the grandparent of anything it creates).
	t.Setenv("HERDER_SPAWNED_BY", "guid-grandparent")
	t.Setenv("HERDER_GUID", "guid-parent")

	// Creator flows pass the session performing the action explicitly — the row
	// must record the parent, not the ambient grandparent (TASK-016).
	if p := BuildProvenance("spawn", "guid-parent", "", t.TempDir(), ""); p.SpawnedBy != "guid-parent" {
		t.Fatalf("explicit spawnedBy = %q, want guid-parent", p.SpawnedBy)
	}
	// Empty spawnedBy harvests the ambient chain — enroll/sidecar rows describe
	// the CURRENT session, whose spawner genuinely is HERDER_SPAWNED_BY.
	if p := BuildProvenance("enroll", "", "", t.TempDir(), ""); p.SpawnedBy != "guid-grandparent" {
		t.Fatalf("ambient spawnedBy = %q, want guid-grandparent", p.SpawnedBy)
	}

	// Ambient chain degrades HERDER_SPAWNED_BY -> HERDER_GUID -> "user".
	t.Setenv("HERDER_SPAWNED_BY", "")
	if p := BuildProvenance("enroll", "", "", t.TempDir(), ""); p.SpawnedBy != "guid-parent" {
		t.Fatalf("ambient guid fallback = %q, want guid-parent", p.SpawnedBy)
	}
	t.Setenv("HERDER_GUID", "")
	if p := BuildProvenance("enroll", "", "", t.TempDir(), ""); p.SpawnedBy != "user" {
		t.Fatalf("ambient user fallback = %q, want user", p.SpawnedBy)
	}
}
