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

const (
	testNodeA = "11111111-1111-4111-8111-111111111111"
	testNodeB = "22222222-2222-4222-8222-222222222222"
	testNodeC = "33333333-3333-4333-8333-333333333333"
)

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

func TestSeatedCandidatesByPaneOrTerminal(t *testing.T) {
	// A reused pane p_1 carries three manual identities: two stale-but-seated
	// (bus_a already superseded on the bus, bus_b likewise) and one live.
	// gamma sits on a different pane; delta is retired on p_1. Candidates must
	// be every seated row on the coordinate, in guid order, and nothing else.
	path := writeRegistry(t,
		`{"kind":"session","guid":"guid-alpha-0000","label":"a","state":"seated","seat":{"pane_id":"p_1","terminal_id":"term_1","hcom_name":"bus_a"}}`,
		`{"kind":"session","guid":"guid-beta-0000","label":"b","state":"seated","seat":{"pane_id":"p_1","terminal_id":"term_1","hcom_name":"bus_b"}}`,
		`{"kind":"session","guid":"guid-gamma-0000","label":"g","state":"seated","seat":{"pane_id":"p_2","terminal_id":"term_2","hcom_name":"bus_g"}}`,
		`{"kind":"session","guid":"guid-delta-0000","label":"d","state":"retired","seat":{"pane_id":"p_1","terminal_id":"term_1","hcom_name":"bus_d"}}`,
		`{"kind":"session","guid":"guid-epsilon-0000","label":"e","state":"unseated","seat":{"pane_id":"p_1","terminal_id":"term_1","hcom_name":"bus_e"}}`,
	)
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"p_1", "term_1"} {
		got := SeatedCandidatesByPaneOrTerminal(recs, key)
		if len(got) != 2 {
			t.Fatalf("SeatedCandidatesByPaneOrTerminal(%q) = %d rows, want 2 (alpha,beta)", key, len(got))
		}
		if ptrString(got[0].GUID) != "guid-alpha-0000" || ptrString(got[1].GUID) != "guid-beta-0000" {
			t.Errorf("candidates(%q) order = %q,%q, want alpha,beta (guid order)", key, ptrString(got[0].GUID), ptrString(got[1].GUID))
		}
	}
	if got := SeatedCandidatesByPaneOrTerminal(recs, "p_2"); len(got) != 1 || ptrString(got[0].GUID) != "guid-gamma-0000" {
		t.Errorf("candidates(p_2) = %+v, want just gamma", got)
	}
	if got := SeatedCandidatesByPaneOrTerminal(recs, ""); got != nil {
		t.Errorf("candidates(\"\") = %+v, want nil", got)
	}
	if got := SeatedCandidatesByPaneOrTerminal(recs, "p_nope"); got != nil {
		t.Errorf("candidates(unknown) = %+v, want nil", got)
	}
}

func TestNonRetiredLabelOwnerUsesLatestRows(t *testing.T) {
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
	owner := NonRetiredLabelOwner(recs, "shared", "guid-alpha-0000")
	if owner == nil || ptrString(owner.GUID) != "guid-gamma-0000" {
		t.Fatalf("NonRetiredLabelOwner(shared, except alpha) = %+v, want gamma", owner)
	}
	if owner := NonRetiredLabelOwner(recs, "shared", "guid-gamma-0000"); owner == nil || ptrString(owner.GUID) != "guid-alpha-0000" {
		t.Fatalf("NonRetiredLabelOwner(shared, except gamma) = %+v, want alpha", owner)
	}
	if owner := NonRetiredLabelOwner(recs, "closed-label", ""); owner != nil {
		t.Fatalf("closed latest row owns label: %+v, want nil", owner)
	}
	if owner := NonRetiredLabelOwner(recs, "", ""); owner != nil {
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
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want node row plus 2 session rows: %s", len(lines), data)
	}
	for i, line := range lines {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if row["kind"] != "node" || row["event"] != "node_registered" || row["node_id"] == "" {
				t.Fatalf("line %d = %s, want node_registered row", i+1, line)
			}
			continue
		}
		if row["kind"] != "session" || row["state"] != "seated" || row["status"] != nil || row["node"] == "" {
			t.Fatalf("line %d = %s, want clean node-attributed seated v2 session row", i+1, line)
		}
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || ptrString(recs[0].GUID) != "g-1" || recs[0].State != v2.StateSeated || recs[0].Status != "" || ptrString(recs[1].GUID) != "g-2" {
		t.Fatalf("four-state view = %+v, want seated g-1/g-2 records", recs)
	}
}

func TestLockedWriteMintsNodeOnceAndStampsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "registry.jsonl")
	for _, guid := range []string{"guid-alpha", "guid-beta"} {
		guid := guid
		if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
			return []v2.SessionRecord{{GUID: guid, Label: guid, State: v2.StateSeated}}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	proj := loadProjection(t, path)
	nodes := proj.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("nodes = %+v, want exactly one minted node", nodes)
	}
	marker, err := os.ReadFile(NodeMarkerPath(path))
	if err != nil {
		t.Fatal(err)
	}
	nodeID := strings.TrimSpace(string(marker))
	if nodeID == "" || nodeID != nodes[0].NodeID {
		t.Fatalf("marker = %q nodes = %+v, want agreement", marker, nodes)
	}
	for _, rec := range proj.Sessions() {
		if rec.Node != nodeID {
			t.Fatalf("session %s node = %q, want %q", rec.GUID, rec.Node, nodeID)
		}
	}
}

func TestLockedWriteReturnsPerCandidateOutcomes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "registry.jsonl")
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{
			{GUID: "guid-shared", Label: "shared", Event: "registered", State: v2.StateSeated},
			{GUID: "guid-shared", Label: "shared", Event: "registered", State: v2.StateSeated},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %+v, want one per candidate", outcomes)
	}
	if outcomes[0].Status != WriteApplied || len(outcomes[0].Row) == 0 || outcomes[0].Reason != "" {
		t.Fatalf("applied outcome = %+v", outcomes[0])
	}
	if outcomes[1].Status != WriteNoop || len(outcomes[1].Row) != 0 || outcomes[1].Reason != "" {
		t.Fatalf("noop outcome = %+v", outcomes[1])
	}
}

func TestLockedWriteRefusalIsAtomicAndReportedPerCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "registry.jsonl")
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-existing", Label: "taken", State: v2.StateSeated}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	before := mustReadFile(t, path)

	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{
			{GUID: "guid-valid", Label: "valid", State: v2.StateSeated},
			{GUID: "guid-refused", Label: "taken", State: v2.StateSeated},
			{GUID: "guid-third", Label: "third", State: v2.StateSeated},
		}, nil
	})
	if err != nil {
		t.Fatalf("candidate refusal returned batch error: %v", err)
	}
	if len(outcomes) != 3 {
		t.Fatalf("outcomes = %+v, want one per candidate", outcomes)
	}
	for i, outcome := range outcomes {
		if outcome.Status != WriteRefused || outcome.Reason == "" || len(outcome.Row) != 0 {
			t.Fatalf("outcomes[%d] = %+v, want refused with reason and no row", i, outcome)
		}
	}
	if !strings.Contains(outcomes[0].Reason, "batch refused atomically") || !strings.Contains(outcomes[1].Reason, `label "taken" already belongs`) {
		t.Fatalf("outcomes = %+v, want atomic-block reason followed by candidate-specific reason", outcomes)
	}
	if !strings.Contains(outcomes[2].Reason, "candidate was not evaluated") {
		t.Fatalf("outcomes[2] = %+v, want unevaluated reason", outcomes[2])
	}
	if got := mustReadFile(t, path); !bytes.Equal(got, before) {
		t.Fatalf("registry changed after atomic refusal:\nbefore=%s\nafter=%s", before, got)
	}
}

func TestLockedWriteRefusesLegacyV1AppendToMintedRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "registry.jsonl")
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-healthy-1", Label: "healthy-1", State: v2.StateSeated}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	before := mustReadFile(t, path)

	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{
			{GUID: "guid-healthy-2", Label: "healthy-2", State: v2.StateSeated},
			{
				GUID:     "guid-poison",
				Event:    "legacy_v1_mapped",
				State:    v2.StateUnseated,
				Label:    "poison",
				Tool:     "claude",
				LegacyV1: true,
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("batch error = %v, want typed refusal outcomes", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %+v, want one per candidate", outcomes)
	}
	err = outcomes[1].Err()
	var legacyErr *LegacyV1AppendError
	if !errors.As(err, &legacyErr) || legacyErr.GUID != "guid-poison" {
		t.Fatalf("err = %v, want LegacyV1AppendError for guid-poison", err)
	}
	for _, want := range []string{"v1-shaped append", "older than this registry schema", "spawner HERDER_BIN", "upgrade the checkout", "excise the on-disk v1-shaped row"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want message containing %q", err, want)
		}
	}
	if got := mustReadFile(t, path); !bytes.Equal(got, before) {
		t.Fatalf("registry changed after refused legacy append:\nbefore=%s\nafter=%s", before, got)
	}

	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-healthy-2", Label: "healthy-2", State: v2.StateSeated}}, nil
	}); err != nil {
		t.Fatalf("healthy write after refused legacy append failed: %v", err)
	}
	if latest := V2ByGUID(loadProjection(t, path), "guid-healthy-2"); latest == nil || latest.State != v2.StateSeated {
		t.Fatalf("healthy write after refusal missing/latest = %+v", latest)
	}
}

func TestLockedWriteRefusesInjectedLegacyV1RowInBornV2Registry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "registry.jsonl")
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-healthy", Label: "healthy", State: v2.StateSeated}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mustMigrationArchivePath(t, path)); !os.IsNotExist(err) {
		t.Fatalf("migration archive exists before poison injection: %v", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	poison := []byte(`{"guid":"guid-poison","label":"poison","role":"worker","agent":"claude","status":"active"}` + "\n")
	if _, err := f.Write(poison); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	poisonedLive := mustReadFile(t, path)
	callbackRan := false

	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		callbackRan = true
		return nil, nil
	})
	if len(outcomes) != 0 {
		t.Fatalf("outcomes = %+v, want none for batch refusal", outcomes)
	}
	var legacyErr *LegacyV1AppendError
	if !errors.As(err, &legacyErr) || legacyErr.GUID != "guid-poison" {
		t.Fatalf("err = %v, want LegacyV1AppendError for guid-poison", err)
	}
	for _, want := range []string{"older than this registry schema", "excise the on-disk v1-shaped row"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want message containing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "remove the archive") {
		t.Fatalf("err = %v, must not advise removing an archive", err)
	}
	if callbackRan {
		t.Fatal("write callback ran despite poisoned born-v2 registry")
	}
	if got := mustReadFile(t, path); !bytes.Equal(got, poisonedLive) {
		t.Fatalf("registry changed after refused migration:\nbefore=%s\nafter=%s", poisonedLive, got)
	}
	if _, err := os.Stat(mustMigrationArchivePath(t, path)); !os.IsNotExist(err) {
		t.Fatalf("migration archive was minted from poisoned born-v2 registry: %v", err)
	}
}

func TestLockedWriteRefusesLegacyV1RowInPlantedMigrationArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "registry.jsonl")
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-healthy", Label: "healthy", State: v2.StateSeated}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	poison := []byte(`{"guid":"guid-poison","label":"poison","role":"worker","agent":"claude","status":"active"}` + "\n")
	if _, err := f.Write(poison); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	poisonedLive := mustReadFile(t, path)
	archive := mustMigrationArchivePath(t, path)
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, poisonedLive, 0o444); err != nil {
		t.Fatal(err)
	}
	callbackRan := false

	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		callbackRan = true
		return nil, nil
	})
	if len(outcomes) != 0 {
		t.Fatalf("outcomes = %+v, want none for batch refusal", outcomes)
	}
	var legacyErr *LegacyV1AppendError
	if !errors.As(err, &legacyErr) || legacyErr.GUID != "guid-poison" {
		t.Fatalf("err = %v, want LegacyV1AppendError for guid-poison", err)
	}
	for _, want := range []string{"migration archive", "v1-shaped session row", "restore the archive", "excise post-mint v1-shaped rows"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want message containing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "remove the archive") {
		t.Fatalf("err = %v, must not advise removing an archive", err)
	}
	if callbackRan {
		t.Fatal("write callback ran despite poisoned migration archive")
	}
	if got := mustReadFile(t, path); !bytes.Equal(got, poisonedLive) {
		t.Fatalf("registry changed after archive refusal:\nbefore=%s\nafter=%s", poisonedLive, got)
	}
	if got := mustReadFile(t, archive); !bytes.Equal(got, poisonedLive) {
		t.Fatalf("migration archive changed after refusal:\nbefore=%s\nafter=%s", poisonedLive, got)
	}
}

func TestTwoProcessFirstWritersConvergeOnOneNode(t *testing.T) {
	if os.Getenv("HERDER_REGISTRY_NODE_HELPER") == "1" {
		runNodeMintHelper()
		return
	}
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	start := filepath.Join(t.TempDir(), "start")
	cmds := make([]*exec.Cmd, 0, 2)
	for _, guid := range []string{"guid-alpha", "guid-beta"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestTwoProcessFirstWritersConvergeOnOneNode$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			"HERDER_REGISTRY_NODE_HELPER=1",
			"HERDER_REGISTRY_NODE_PATH="+path,
			"HERDER_REGISTRY_NODE_GUID="+guid,
			"HERDER_REGISTRY_NODE_START="+start,
		)
		cmds = append(cmds, cmd)
		if err := cmd.Start(); err != nil {
			t.Fatalf("start helper %s: %v", guid, err)
		}
		t.Cleanup(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
	}
	if err := os.WriteFile(start, []byte("go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper failed: %v", err)
		}
	}
	proj := loadProjection(t, path)
	if got := len(proj.Nodes()); got != 1 {
		t.Fatalf("node rows = %d, want 1", got)
	}
	nodeID := proj.Nodes()[0].NodeID
	for _, rec := range proj.Sessions() {
		if rec.Node != nodeID {
			t.Fatalf("session %s node = %q, want %q", rec.GUID, rec.Node, nodeID)
		}
	}
}

func runNodeMintHelper() {
	start := os.Getenv("HERDER_REGISTRY_NODE_START")
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
	path := os.Getenv("HERDER_REGISTRY_NODE_PATH")
	guid := os.Getenv("HERDER_REGISTRY_NODE_GUID")
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: guid, Label: guid, State: v2.StateSeated}}, nil
	})
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(2)
	}
	if outcome, oneErr := SingleOutcome(outcomes); oneErr != nil || outcome.Err() != nil {
		if oneErr != nil {
			os.Stderr.WriteString(oneErr.Error() + "\n")
		} else {
			os.Stderr.WriteString(outcome.Err().Error() + "\n")
		}
		os.Exit(2)
	}
}

func TestLockedWriteRefusesHalfPresentNodeState(t *testing.T) {
	t.Run("marker only", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "registry.jsonl")
		if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
			return []v2.SessionRecord{{GUID: "guid-marker", State: v2.StateSeated}}, nil
		})
		if len(outcomes) != 0 {
			t.Fatalf("outcomes = %+v, want none for batch refusal", outcomes)
		}
		if err == nil || !strings.Contains(err.Error(), "herder node init") {
			t.Fatalf("err = %v, want node init guidance", err)
		}
	})
	t.Run("row only", func(t *testing.T) {
		path := writeRegistry(t, `{"kind":"node","event":"node_registered","node_id":"`+testNodeB+`","recorded_at":"2026-07-08T00:00:00Z"}`)
		outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
			return []v2.SessionRecord{{GUID: "guid-row", State: v2.StateSeated}}, nil
		})
		if len(outcomes) != 0 {
			t.Fatalf("outcomes = %+v, want none for batch refusal", outcomes)
		}
		if err == nil || !strings.Contains(err.Error(), "herder node init") {
			t.Fatalf("err = %v, want node init guidance", err)
		}
	})
	t.Run("empty marker", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "registry.jsonl")
		if err := os.WriteFile(NodeMarkerPath(path), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
			return []v2.SessionRecord{{GUID: "guid-empty", State: v2.StateSeated}}, nil
		})
		if len(outcomes) != 0 {
			t.Fatalf("outcomes = %+v, want none for batch refusal", outcomes)
		}
		if err == nil || !strings.Contains(err.Error(), "herder node init") {
			t.Fatalf("err = %v, want node init guidance", err)
		}
	})
}

func TestNodeInitRepairsAndCloneRepairKeepsPriorRows(t *testing.T) {
	t.Run("idempotent healthy state", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "registry.jsonl")
		first, err := InitNode(path, false)
		if err != nil {
			t.Fatal(err)
		}
		second, err := InitNode(path, false)
		if err != nil {
			t.Fatal(err)
		}
		if first.NodeID == "" || second.NodeID != first.NodeID || second.Changed {
			t.Fatalf("first=%+v second=%+v, want stable no-op second init", first, second)
		}
		if got := len(loadProjection(t, path).Nodes()); got != 1 {
			t.Fatalf("node rows = %d, want 1 after idempotent init", got)
		}
	})
	t.Run("marker only", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "registry.jsonl")
		if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := InitNode(path, false)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Changed || res.NodeID != testNodeA || !hasNode(loadProjection(t, path).Nodes(), testNodeA) {
			t.Fatalf("res=%+v, want marker node row repair", res)
		}
	})
	t.Run("row only", func(t *testing.T) {
		path := writeRegistry(t, `{"kind":"node","event":"node_registered","node_id":"`+testNodeB+`","recorded_at":"2026-07-08T00:00:00Z"}`)
		res, err := InitNode(path, false)
		if err != nil {
			t.Fatal(err)
		}
		marker, err := os.ReadFile(NodeMarkerPath(path))
		if err != nil {
			t.Fatal(err)
		}
		if !res.Changed || res.NodeID != testNodeB || strings.TrimSpace(string(marker)) != testNodeB {
			t.Fatalf("res=%+v marker=%q, want marker repair", res, marker)
		}
	})
	t.Run("malformed marker refuses", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "registry.jsonl")
		if err := os.WriteFile(NodeMarkerPath(path), []byte("not-a-node-id\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"kind":"node","event":"node_registered","node_id":"`+testNodeB+`","recorded_at":"2026-07-08T00:00:00Z"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, forceNew := range []bool{false, true} {
			if _, err := InitNode(path, forceNew); err == nil || !strings.Contains(err.Error(), NodeMarkerPath(path)) || !strings.Contains(err.Error(), "restore it from registry") {
				t.Fatalf("forceNew=%v err = %v, want malformed marker refusal with restore guidance", forceNew, err)
			}
		}
	})
	t.Run("conflicting marker multiple nodes refuses", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "registry.jsonl")
		if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(
			`{"kind":"node","event":"node_registered","node_id":"`+testNodeB+`","recorded_at":"2026-07-08T00:00:00Z"}`+"\n"+
				`{"kind":"node","event":"node_registered","node_id":"`+testNodeC+`","recorded_at":"2026-07-08T00:00:01Z"}`+"\n",
		), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := InitNode(path, false); err == nil || !strings.Contains(err.Error(), "registry node init refused") || !strings.Contains(err.Error(), testNodeB) || !strings.Contains(err.Error(), testNodeC) {
			t.Fatalf("err = %v, want conflicting marker/multiple-node refusal", err)
		}
	})
	t.Run("empty marker", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "registry.jsonl")
		if err := os.WriteFile(NodeMarkerPath(path), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := InitNode(path, false)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Changed || res.NodeID == "" || len(loadProjection(t, path).Nodes()) != 1 {
			t.Fatalf("res=%+v, want fresh repair for empty marker", res)
		}
	})
	t.Run("empty marker new", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "registry.jsonl")
		if err := os.WriteFile(NodeMarkerPath(path), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := InitNode(path, true)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Changed || res.NodeID == "" || strings.TrimSpace(string(mustReadFile(t, NodeMarkerPath(path)))) != res.NodeID {
			t.Fatalf("res=%+v, want --new repair for empty marker", res)
		}
	})
	t.Run("new clone node", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "registry.jsonl")
		if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
			return []v2.SessionRecord{{GUID: "guid-old", State: v2.StateSeated}}, nil
		}); err != nil {
			t.Fatal(err)
		}
		oldProj := loadProjection(t, path)
		oldNode := oldProj.Nodes()[0].NodeID
		if _, err := InitNode(path, true); err != nil {
			t.Fatal(err)
		}
		if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
			return []v2.SessionRecord{{GUID: "guid-new", State: v2.StateSeated}}, nil
		}); err != nil {
			t.Fatal(err)
		}
		proj := loadProjection(t, path)
		nodes := proj.Nodes()
		if len(nodes) != 2 {
			t.Fatalf("nodes = %+v, want old and fresh node", nodes)
		}
		newNode := strings.TrimSpace(string(mustReadFile(t, NodeMarkerPath(path))))
		if newNode == "" || newNode == oldNode {
			t.Fatalf("new marker = %q old = %q, want fresh", newNode, oldNode)
		}
		byGUID := map[string]v2.SessionRecord{}
		for _, rec := range proj.Sessions() {
			byGUID[rec.GUID] = rec
		}
		if byGUID["guid-old"].Node != oldNode {
			t.Fatalf("old row node = %q, want %q", byGUID["guid-old"].Node, oldNode)
		}
		if byGUID["guid-new"].Node != newNode {
			t.Fatalf("new row node = %q, want %q", byGUID["guid-new"].Node, newNode)
		}
	})
}

func TestCloneRepairLifecycleWritesStampFreshNode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{
			{GUID: "guid-cull", State: v2.StateSeated, Label: "cull-me", Role: "worker", Tool: "codex", Seat: &v2.Seat{Kind: "herdr", TerminalID: "term_cull"}},
			{GUID: "guid-rename", State: v2.StateSeated, Label: "old-name", Role: "worker", Tool: "codex", Seat: &v2.Seat{Kind: "herdr", TerminalID: "term_rename"}},
			{GUID: "guid-recognise", State: v2.StateSeated, Label: "recognise-me", Role: "worker", Tool: "codex", Seat: &v2.Seat{Kind: "herdr", TerminalID: "term_old"}},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	oldNode := strings.TrimSpace(string(mustReadFile(t, NodeMarkerPath(path))))
	if _, err := InitNode(path, true); err != nil {
		t.Fatal(err)
	}
	newNode := strings.TrimSpace(string(mustReadFile(t, NodeMarkerPath(path))))
	if newNode == oldNode {
		t.Fatalf("clone repair marker = old node %q, want fresh", oldNode)
	}

	if err := updateLockedForTest(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		current := V2ByGUID(tx.Projection, "guid-cull")
		next := *current
		next.Event = "unseated"
		next.RecordedAt = ""
		next.State = v2.StateUnseated
		next.Seat = nil
		return []v2.SessionRecord{next}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := updateLockedForTest(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		current := V2ByGUID(tx.Projection, "guid-rename")
		next := *current
		next.Event = "labelled"
		next.RecordedAt = ""
		next.Label = "new-name"
		return []v2.SessionRecord{next}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := updateLockedForTest(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		current := V2ByGUID(tx.Projection, "guid-recognise")
		next := *current
		next.Event = "recognised"
		next.RecordedAt = ""
		next.Seat = &v2.Seat{Kind: "herdr", TerminalID: "term_new", PaneID: "p_new"}
		return []v2.SessionRecord{next}, nil
	}); err != nil {
		t.Fatal(err)
	}

	proj := loadProjection(t, path)
	for _, guid := range []string{"guid-cull", "guid-rename", "guid-recognise"} {
		rec := V2ByGUID(proj, guid)
		if rec == nil {
			t.Fatalf("missing %s", guid)
		}
		if rec.Node != newNode {
			t.Fatalf("%s node = %q, want fresh node %q", guid, rec.Node, newNode)
		}
		if rec.Seat != nil && rec.Seat.Node != newNode {
			t.Fatalf("%s seat node = %q, want fresh node %q", guid, rec.Seat.Node, newNode)
		}
	}
	lines := strings.Split(strings.TrimSpace(string(mustReadFile(t, path))), "\n")
	oldRows := 0
	for _, line := range lines {
		if strings.Contains(line, `"node":"`+oldNode+`"`) {
			oldRows++
		}
	}
	if oldRows != 3 {
		t.Fatalf("old-node session rows = %d, want original three rows untouched", oldRows)
	}
}

func TestUnknownNodeRowsAreReadOnlyButDoNotBlockLocalWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(
		`{"kind":"node","event":"node_registered","node_id":"`+testNodeA+`","recorded_at":"2026-07-08T00:00:00Z"}`+"\n"+
			`{"kind":"session","guid":"guid-ghost","event":"registered","recorded_at":"2026-07-08T00:00:01Z","node":"`+testNodeB+`","state":"seated","label":"ghost","role":"worker","tool":"codex","seat":{"kind":"herdr","node":"`+testNodeB+`","terminal_id":"term_ghost"}}`+"\n"+
			`{"kind":"session","guid":"guid-local","event":"registered","recorded_at":"2026-07-08T00:00:02Z","node":"`+testNodeA+`","state":"seated","label":"local","role":"worker","tool":"codex","seat":{"kind":"herdr","node":"`+testNodeA+`","terminal_id":"term_local"}}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := loadProjection(t, path)
	if got := proj.Anomalies(); len(got) != 1 || got[0].Type != "unknown-node" || got[0].GUID != "guid-ghost" {
		t.Fatalf("anomalies = %+v, want unknown-node for guid-ghost", got)
	}
	outcomes, err := UpdateLocked(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		current := V2ByGUID(tx.Projection, "guid-ghost")
		next := *current
		next.Event = "labelled"
		next.Label = "ghost-new"
		return []v2.SessionRecord{next}, nil
	})
	if err == nil {
		outcome, oneErr := SingleOutcome(outcomes)
		if oneErr != nil {
			err = oneErr
		} else {
			err = outcome.Err()
		}
	}
	if err == nil || !strings.Contains(err.Error(), "unknown node") {
		t.Fatalf("err = %v, want unknown-node mutation refusal", err)
	}
	if err := updateLockedForTest(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		current := V2ByGUID(tx.Projection, "guid-local")
		next := *current
		next.Event = "labelled"
		next.RecordedAt = ""
		next.Label = "ghost"
		return []v2.SessionRecord{next}, nil
	}); err != nil {
		t.Fatalf("local write using unknown row label should still work: %v", err)
	}
	latest := V2ByGUID(loadProjection(t, path), "guid-local")
	if latest.Label != "ghost" || latest.Node != testNodeA {
		t.Fatalf("latest local row = %+v, want label ghost stamped local node", latest)
	}
}

func TestLoadPreservesFourStateViewFromV2Rows(t *testing.T) {
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
	if got := byGUID["guid-seated"]; got.State != v2.StateSeated || got.Status != "" || got.PaneID != "p_1" || got.TerminalID != "term_1" || got.HcomName != "bus-seat" || got.HcomDir != "/hcom" || got.Agent != "codex" {
		t.Fatalf("seated view = %+v", got)
	}
	if got := byGUID["guid-unseated"]; got.State != v2.StateUnseated || got.Status != "" || got.PaneID != "" || got.Agent != "claude" {
		t.Fatalf("unseated view = %+v", got)
	}
	if got := byGUID["guid-retired"]; got.State != v2.StateRetired || got.Status != "" || got.Agent != "bash" {
		t.Fatalf("retired view = %+v", got)
	}
}

func TestReadPredicatesKeepSeatAndLeaseQuestionsSeparate(t *testing.T) {
	rows := []Record{
		{State: v2.StateSeated},
		{State: v2.StateUnseated},
		{State: v2.StateRetired},
		{State: v2.StateLost},
		{},
	}
	wantSeated := []bool{true, false, false, false, false}
	wantNonRetired := []bool{true, true, false, false, false}
	for i, row := range rows {
		if got := IsSeated(row); got != wantSeated[i] {
			t.Errorf("row %d IsSeated = %v, want %v", i, got, wantSeated[i])
		}
		if got := IsNonRetired(row); got != wantNonRetired[i] {
			t.Errorf("row %d IsNonRetired = %v, want %v", i, got, wantNonRetired[i])
		}
	}
}

func TestDecodeLegacyV1RawReadsCoordinatesWithoutMappingV2State(t *testing.T) {
	rec := v2.SessionRecord{
		LegacyV1: true,
		State:    v2.StateUnseated,
		Raw:      json.RawMessage(`{"guid":"guid-old","status":"active","pane_id":"p_old","terminal_id":"term_old","hcom_name":"bus-old","hcom_dir":"/old-bus"}`),
	}
	got, ok := DecodeLegacyV1Raw(rec)
	if !ok {
		t.Fatal("DecodeLegacyV1Raw did not decode a legacy row")
	}
	if got.V1Status != "active" || got.PaneID != "p_old" || got.TerminalID != "term_old" || got.HcomName != "bus-old" || got.HcomDir != "/old-bus" {
		t.Fatalf("compat = %+v, want original v1 status and coordinates", got)
	}
	if _, ok := DecodeLegacyV1Raw(v2.SessionRecord{State: v2.StateSeated, Raw: rec.Raw}); ok {
		t.Fatal("v2 row was accepted by legacy-v1 compatibility decoder")
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
	if owner := NonRetiredLabelOwner(recs, "shared", ""); owner == nil {
		t.Fatal("non-retired resolver sees no shared owner")
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
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{V2FromRecord(rec, "registered", v2.StateSeated, "")}, nil
	})
	if err == nil {
		outcome, oneErr := SingleOutcome(outcomes)
		if oneErr != nil {
			err = oneErr
		} else {
			err = outcome.Err()
		}
	}
	if err == nil {
		os.Exit(0)
	}
	os.Stderr.WriteString(err.Error() + "\n")
	if strings.Contains(err.Error(), `label "shared" already belongs`) {
		os.Exit(42)
	}
	os.Exit(2)
}

func TestLockedValidatorMigratesLegacyActiveDormantOnRename(t *testing.T) {
	path := writeRegistry(t, `{"guid":"guid-legacy","short_guid":"legacy","label":"old","role":"worker","agent":"codex","terminal_id":"term_OLD","pane_id":"p_old","hcom_dir":"/hcom","hcom_name":"bus-old","status":"active"}`)
	if err := updateLockedForTest(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
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
	if latest == nil || ptrString(latest.Label) != "new" || latest.PaneID != "" || latest.TerminalID != "" || latest.HcomName != "" || latest.HcomDir != "" {
		t.Fatalf("latest = %+v, want renamed dormant row without legacy seat", latest)
	}
}

func TestLegacyV1MigrationArchivesAndReseeds(t *testing.T) {
	path := copyRegistryFixture(t, "v1-real-shape.jsonl")
	original := mustReadFile(t, path)
	before := loadProjection(t, path)
	if len(before.Nodes()) != 0 {
		t.Fatalf("genuine v1 fixture has v2 node rows: %+v", before.Nodes())
	}
	if _, err := os.Stat(mustMigrationArchivePath(t, path)); !os.IsNotExist(err) {
		t.Fatalf("genuine v1 fixture has a prior migration archive: %v", err)
	}
	seenMigrated := false
	if err := updateLockedForTest(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		if current := V2ByGUID(tx.Projection, "2447b0e6-5004-4aca-84cd-08d7798dad52"); current == nil || current.LegacyV1 || current.State != v2.StateUnseated {
			t.Fatalf("migrated projection current = %+v, want dormant v2 row", current)
		}
		seenMigrated = true
		return []v2.SessionRecord{{GUID: "guid-new", Event: "registered", State: v2.StateSeated, Label: "new", Tool: "codex"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !seenMigrated {
		t.Fatal("write callback did not run")
	}
	archive := mustMigrationArchivePath(t, path)
	if got := mustReadFile(t, archive); !bytes.Equal(got, original) {
		t.Fatalf("archive bytes changed\narchive=%s\noriginal=%s", got, original)
	}
	proj := loadProjection(t, path)
	if got := len(proj.Nodes()); got != 1 {
		t.Fatalf("nodes = %d, want 1", got)
	}
	if got := len(proj.Namespaces()); got != 2 {
		t.Fatalf("namespaces = %+v, want default and teams hcom dirs", proj.Namespaces())
	}
	byGUID := map[string]v2.SessionRecord{}
	for _, rec := range proj.Sessions() {
		if rec.LegacyV1 {
			t.Fatalf("live file still has legacy row: %+v", rec)
		}
		byGUID[rec.GUID] = rec
	}
	if _, ok := byGUID["366fb03a-2f91-47f8-8a6c-eee954e413a5"]; ok {
		t.Fatalf("closed legacy guid was reseeded live: %+v", byGUID["366fb03a-2f91-47f8-8a6c-eee954e413a5"])
	}
	corpse := byGUID["2447b0e6-5004-4aca-84cd-08d7798dad52"]
	if corpse.Event != migrationEventV1 || corpse.State != v2.StateUnseated || corpse.Seat != nil || corpse.Node == "" {
		t.Fatalf("corpse = %+v, want migrated dormant node-stamped row", corpse)
	}
	team := byGUID["24cb80b1-852f-4d30-8f78-e241aaf7c97e"]
	if team.State != v2.StateUnseated || len(team.SIDs) != 1 || team.Continuity != "confirmed" {
		t.Fatalf("team = %+v, want dormant sid-seeded confirmed row", team)
	}
	if byGUID["guid-new"].Node == "" {
		t.Fatalf("new row = %+v, want node-stamped append after migration", byGUID["guid-new"])
	}
}

func TestLegacyV1MigrationTwiceIsByteStable(t *testing.T) {
	path := copyRegistryFixture(t, "v1-real-shape.jsonl")
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	once := mustReadFile(t, path)
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	twice := mustReadFile(t, path)
	if !bytes.Equal(once, twice) {
		t.Fatalf("migrate twice changed live file\nonce=%s\ntwice=%s", once, twice)
	}
}

func TestLegacyV1MigrationRecoversEmptyLiveFromArchive(t *testing.T) {
	sourcePath := copyRegistryFixture(t, "v1-real-shape.jsonl")
	source := mustReadFile(t, sourcePath)
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	archive := mustMigrationArchivePath(t, path)
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, source, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, archive); !bytes.Equal(got, source) {
		t.Fatalf("archive changed during recovery")
	}
	proj := loadProjection(t, path)
	if got := len(proj.Sessions()); got != 3 {
		t.Fatalf("sessions = %+v, want three non-retired recovered sessions", proj.Sessions())
	}
}

func TestLegacyV1MigrationRecoversPartialLiveWithNodeFromArchive(t *testing.T) {
	sourcePath := copyRegistryFixture(t, "v1-real-shape.jsonl")
	source := mustReadFile(t, sourcePath)
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	archive := mustMigrationArchivePath(t, path)
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, source, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	partial := strings.Join([]string{
		`{"kind":"node","event":"node_registered","node_id":"` + testNodeA + `","recorded_at":"2026-07-08T00:00:00Z"}`,
		`{"kind":"session","guid":"2447b0e6-5004-4aca-84cd-08d7798dad52","event":"migrated_v1","recorded_at":"2026-07-08T00:00:01Z","node":"` + testNodeA + `","state":"unseated","label":"partial","role":"worker","tool":"claude","continuity":"assumed"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	proj := loadProjection(t, path)
	if got := len(proj.Nodes()); got < 1 {
		t.Fatalf("nodes = %+v, want node row recovered", proj.Nodes())
	}
	for _, rec := range proj.Sessions() {
		if rec.Node == "" || !hasNode(proj.Nodes(), rec.Node) {
			t.Fatalf("session %s node = %q nodes=%+v, want registered node attribution", rec.GUID, rec.Node, proj.Nodes())
		}
	}
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-after-recovery", Event: "registered", State: v2.StateSeated, Label: "after", Tool: "codex"}}, nil
	}); err != nil {
		t.Fatalf("next locked write after recovery failed: %v", err)
	}
}

func TestLegacyV1MigrationRefusesMismatchedExistingArchive(t *testing.T) {
	path := copyRegistryFixture(t, "v1-real-shape.jsonl")
	original := mustReadFile(t, path)
	archive := mustMigrationArchivePath(t, path)
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, original[:len(original)/2], 0o444); err != nil {
		t.Fatal(err)
	}
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil })
	if len(outcomes) != 0 {
		t.Fatalf("outcomes = %+v, want none for batch refusal", outcomes)
	}
	if err == nil || !strings.Contains(err.Error(), "existing archive") || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v, want mismatched archive refusal", err)
	}
	if strings.Contains(err.Error(), "remove the archive before retrying") || !strings.Contains(err.Error(), "do not remove the archive") || !strings.Contains(err.Error(), "excise post-mint v1-shaped rows") {
		t.Fatalf("err = %v, want safe recovery text without archive-removal guidance", err)
	}
	if got := mustReadFile(t, path); !bytes.Equal(got, original) {
		t.Fatalf("live registry was changed after archive refusal")
	}
}

func TestRotationAtThresholdArchivesAndReseeds(t *testing.T) {
	t.Setenv(rotationThresholdEnv, "650")
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "registry.jsonl.archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "registry.jsonl.archive", "0001-v1-migration.jsonl"), []byte(`{"guid":"legacy","status":"active"}`+"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	original := strings.Join([]string{
		`{"kind":"node","event":"node_registered","node_id":"` + testNodeA + `","recorded_at":"2026-07-08T00:00:00Z"}`,
		`{"kind":"session","guid":"guid-keep","event":"registered","recorded_at":"2026-07-08T00:00:01Z","node":"` + testNodeA + `","state":"seated","label":"keep-old","role":"worker","tool":"codex","seat":{"kind":"herdr","node":"` + testNodeA + `","terminal_id":"term_old","pane_id":"p_old","hcom_name":"keep-bus","namespace":"/hcom","confirmed_at":"2026-07-08T00:00:01Z"}}`,
		`{"kind":"session","guid":"guid-drop","event":"retired","recorded_at":"2026-07-08T00:00:02Z","node":"` + testNodeA + `","state":"retired","label":"drop","role":"worker","tool":"claude"}`,
		`{"kind":"session","guid":"guid-keep","event":"labelled","recorded_at":"2026-07-08T00:00:03Z","node":"` + testNodeA + `","state":"seated","label":"keep","role":"worker","tool":"codex","seat":{"kind":"herdr","node":"` + testNodeA + `","terminal_id":"term_new","pane_id":"p_new","hcom_name":"keep-bus","namespace":"/hcom","confirmed_at":"2026-07-08T00:00:03Z"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateLockedForTest(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		if current := V2ByGUID(tx.Projection, "guid-drop"); current != nil {
			t.Fatalf("retired row visible after rotation = %+v", current)
		}
		return []v2.SessionRecord{{GUID: "guid-new", Event: "registered", State: v2.StateSeated, Label: "new", Tool: "bash"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "registry.jsonl.archive", "0002-rotation.jsonl")
	if got := mustReadFile(t, archive); string(got) != original {
		t.Fatalf("rotation archive changed\narchive=%s\noriginal=%s", got, original)
	}
	if mode := fileMode(t, archive); mode != 0o444 {
		t.Fatalf("archive mode = %o, want 0444", mode)
	}
	proj := loadProjection(t, path)
	if got := V2ByGUID(proj, "guid-keep"); got == nil || got.Label != "keep" || got.Seat == nil || got.Seat.PaneID != "p_new" {
		t.Fatalf("kept row = %+v, want latest self-contained snapshot", got)
	}
	if got := V2ByGUID(proj, "guid-drop"); got != nil {
		t.Fatalf("retired row reseeded live: %+v", got)
	}
	if got := V2ByGUID(proj, "guid-new"); got == nil || got.Node != testNodeA {
		t.Fatalf("new row = %+v, want post-rotation append node-stamped", got)
	}
	if got := len(registryArchivePathsForTest(t, path)); got != 2 {
		t.Fatalf("archives = %d, want 2", got)
	}
	once := mustReadFile(t, path)
	t.Setenv(rotationThresholdEnv, "1048576")
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if twice := mustReadFile(t, path); !bytes.Equal(once, twice) {
		t.Fatalf("under-threshold recheck changed live\nonce=%s\ntwice=%s", once, twice)
	}
	if got := len(registryArchivePathsForTest(t, path)); got != 2 {
		t.Fatalf("archives after under-threshold write = %d, want 2", got)
	}
}

func TestRotationRecoversPartialLiveFromArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := strings.Join([]string{
		`{"kind":"node","event":"node_registered","node_id":"` + testNodeA + `","recorded_at":"2026-07-08T00:00:00Z"}`,
		`{"kind":"session","guid":"guid-a","event":"registered","recorded_at":"2026-07-08T00:00:01Z","node":"` + testNodeA + `","state":"seated","label":"a","role":"worker","tool":"codex"}`,
		`{"kind":"session","guid":"guid-b","event":"registered","recorded_at":"2026-07-08T00:00:02Z","node":"` + testNodeA + `","state":"unseated","label":"b","role":"worker","tool":"claude"}`,
	}, "\n") + "\n"
	archive := filepath.Join(dir, "registry.jsonl.archive", "0002-rotation.jsonl")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte(source), 0o444); err != nil {
		t.Fatal(err)
	}
	partial := `{"kind":"node","event":"node_registered","node_id":"` + testNodeA + `","recorded_at":"2026-07-08T00:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	proj := loadProjection(t, path)
	if V2ByGUID(proj, "guid-a") == nil || V2ByGUID(proj, "guid-b") == nil {
		t.Fatalf("recovered sessions = %+v, want guid-a and guid-b", proj.Sessions())
	}
	if got := mustReadFile(t, archive); string(got) != source {
		t.Fatalf("archive changed during recovery")
	}
}

func TestMigrationRecoveryDoesNotRefireOnPureV2LiveWithStaleMigrationArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "registry.jsonl.archive", "0001-v1-migration.jsonl")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	staleV1 := strings.Join([]string{
		`{"guid":"guid-old-a","label":"old-a","status":"active"}`,
		`{"guid":"guid-old-b","label":"old-b","status":"active"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(archive, []byte(staleV1), 0o444); err != nil {
		t.Fatal(err)
	}
	live := strings.Join([]string{
		`{"kind":"node","event":"node_registered","node_id":"` + testNodeA + `","recorded_at":"2026-07-08T00:00:00Z"}`,
		`{"kind":"session","guid":"guid-current","event":"registered","recorded_at":"2026-07-08T00:00:01Z","node":"` + testNodeA + `","state":"seated","label":"current","role":"worker","tool":"codex"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(live), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-new", Event: "registered", State: v2.StateSeated, Label: "new", Tool: "bash"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	proj := loadProjection(t, path)
	if V2ByGUID(proj, "guid-old-a") != nil || V2ByGUID(proj, "guid-old-b") != nil {
		t.Fatalf("stale migration archive resurrected old rows: %+v", proj.Sessions())
	}
	if V2ByGUID(proj, "guid-current") == nil || V2ByGUID(proj, "guid-new") == nil {
		t.Fatalf("live rows = %+v, want current and new", proj.Sessions())
	}
}

func TestRotationRecoveryUsesNewestRotationArchiveOverMigrationArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	archDir := filepath.Join(dir, "registry.jsonl.archive")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staleMigration := strings.Join([]string{
		`{"guid":"guid-stale","label":"stale","status":"active"}`,
		`{"guid":"guid-retired","label":"retired-before","status":"active"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(archDir, "0001-v1-migration.jsonl"), []byte(staleMigration), 0o444); err != nil {
		t.Fatal(err)
	}
	rotation := strings.Join([]string{
		`{"kind":"node","event":"node_registered","node_id":"` + testNodeA + `","recorded_at":"2026-07-08T00:00:00Z"}`,
		`{"kind":"session","guid":"guid-post","event":"registered","recorded_at":"2026-07-08T00:00:01Z","node":"` + testNodeA + `","state":"seated","label":"post","role":"worker","tool":"codex"}`,
		`{"kind":"session","guid":"guid-retired","event":"retired","recorded_at":"2026-07-08T00:00:02Z","node":"` + testNodeA + `","state":"retired","label":"retired","role":"worker","tool":"claude"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(archDir, "0002-rotation.jsonl"), []byte(rotation), 0o444); err != nil {
		t.Fatal(err)
	}
	partial := `{"kind":"node","event":"node_registered","node_id":"` + testNodeA + `","recorded_at":"2026-07-08T00:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	proj := loadProjection(t, path)
	if V2ByGUID(proj, "guid-post") == nil {
		t.Fatalf("post-migration session missing after recovery: %+v", proj.Sessions())
	}
	if V2ByGUID(proj, "guid-stale") != nil {
		t.Fatalf("stale migration row resurrected: %+v", proj.Sessions())
	}
	if retired := V2ByGUID(proj, "guid-retired"); retired != nil {
		t.Fatalf("retired row reseeded live: %+v", retired)
	}
}

func TestLoadWithArchivesMergesDeterministicallyLiveWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	archDir := filepath.Join(dir, "registry.jsonl.archive")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archDir, "0002-rotation.jsonl"), []byte(`{"guid":"guid-arch","short_guid":"arch","label":"arch-new","status":"closed"}`+"\n"+`{"guid":"guid-collide","short_guid":"collide","label":"arch-collide","status":"closed"}`+"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archDir, "0001-rotation.jsonl"), []byte(`{"guid":"guid-arch","short_guid":"arch","label":"arch-old","status":"active"}`+"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"guid":"guid-collide","short_guid":"collide","label":"live-collide","status":"active"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := LoadWithArchives(path)
	if err != nil {
		t.Fatal(err)
	}
	arch := Resolve(recs, "guid-arch")
	if arch == nil || ptrString(arch.Label) != "arch-new" || !arch.Archived {
		t.Fatalf("archive-only = %+v, want latest archive row marked archived", arch)
	}
	live := Resolve(recs, "guid-collide")
	if live == nil || ptrString(live.Label) != "live-collide" || live.Archived {
		t.Fatalf("live collision = %+v, want live row to win", live)
	}
}

func TestLoadWithArchivesUsesLatestAcrossThreeRotationArchives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	archDir := filepath.Join(dir, "registry.jsonl.archive")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, label := range map[string]string{
		"0002-rotation.jsonl": "two",
		"0003-rotation.jsonl": "three",
		"0004-rotation.jsonl": "four",
	} {
		row := `{"guid":"guid-tie","short_guid":"tie","label":"` + label + `","status":"closed"}` + "\n"
		if err := os.WriteFile(filepath.Join(archDir, name), []byte(row), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := LoadWithArchives(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := Resolve(recs, "guid-tie"); got == nil || ptrString(got.Label) != "four" {
		t.Fatalf("latest archive tie = %+v, want 0004/four", got)
	}
}

func TestRotationReusesMatchingArchiveAfterPreTruncateCrash(t *testing.T) {
	t.Setenv(rotationThresholdEnv, "650")
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := strings.Join([]string{
		`{"kind":"node","event":"node_registered","node_id":"` + testNodeA + `","recorded_at":"2026-07-08T00:00:00Z"}`,
		`{"kind":"session","guid":"guid-live","event":"registered","recorded_at":"2026-07-08T00:00:01Z","node":"` + testNodeA + `","state":"seated","label":"live","role":"worker","tool":"codex"}`,
		`{"kind":"session","guid":"guid-retired","event":"retired","recorded_at":"2026-07-08T00:00:02Z","node":"` + testNodeA + `","state":"retired","label":"` + strings.Repeat("x", 256) + `","role":"worker","tool":"codex"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "registry.jsonl.archive", "0002-rotation.jsonl")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte(source), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-after", Event: "registered", State: v2.StateSeated, Label: "after", Tool: "bash"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if archives := registryArchivePathsForTest(t, path); len(archives) != 1 || filepath.Base(archives[0]) != "0002-rotation.jsonl" {
		t.Fatalf("archives = %+v, want only reused 0002 archive", archives)
	}
}

func TestRotationSkipsWhenReseedWouldStillExceedThreshold(t *testing.T) {
	t.Setenv(rotationThresholdEnv, "10")
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hugeLabel := strings.Repeat("x", 128)
	source := strings.Join([]string{
		`{"kind":"node","event":"node_registered","node_id":"` + testNodeA + `","recorded_at":"2026-07-08T00:00:00Z"}`,
		`{"kind":"session","guid":"guid-huge","event":"registered","recorded_at":"2026-07-08T00:00:01Z","node":"` + testNodeA + `","state":"seated","label":"` + hugeLabel + `","role":"worker","tool":"codex"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{GUID: "guid-after", Event: "registered", State: v2.StateSeated, Label: "after", Tool: "bash"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if archives := registryArchivePathsForTest(t, path); len(archives) != 0 {
		t.Fatalf("archives = %+v, want no rotation because reseed remains over threshold", archives)
	}
}

func TestRotationInvalidThresholdNamesFix(t *testing.T) {
	t.Setenv(rotationThresholdEnv, "not-bytes")
	path := writeRegistry(t, `{"kind":"node","event":"node_registered","node_id":"`+testNodeA+`","recorded_at":"2026-07-08T00:00:00Z"}`)
	if err := os.WriteFile(NodeMarkerPath(path), []byte(testNodeA+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil })
	if len(outcomes) != 0 {
		t.Fatalf("outcomes = %+v, want none for batch refusal", outcomes)
	}
	if err == nil || !strings.Contains(err.Error(), rotationThresholdEnv+`="not-bytes"`) || !strings.Contains(err.Error(), "unset it to use the default") {
		t.Fatalf("err = %v, want variable/value/fix guidance", err)
	}
}

func TestRotationRecoveryRefusalTexts(t *testing.T) {
	for name, tc := range map[string]struct {
		writeArchive func(string)
		want         string
	}{
		"missing": {
			writeArchive: func(path string) {
				if err := os.Symlink("missing-target", path); err != nil {
					t.Fatal(err)
				}
			},
			want: "missing archive",
		},
		"empty": {
			writeArchive: func(path string) {
				if err := os.WriteFile(path, nil, 0o444); err != nil {
					t.Fatal(err)
				}
			},
			want: "is empty",
		},
		"quarantined": {
			writeArchive: func(path string) {
				if err := os.WriteFile(path, []byte("{not-json\n"), 0o444); err != nil {
					t.Fatal(err)
				}
			},
			want: "has quarantined rows",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "registry.jsonl")
			archive := filepath.Join(dir, "registry.jsonl.archive", "0002-rotation.jsonl")
			if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
				t.Fatal(err)
			}
			tc.writeArchive(archive)
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) { return nil, nil })
			if len(outcomes) != 0 {
				t.Fatalf("outcomes = %+v, want none for batch refusal", outcomes)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRotationArchiveByteVerificationRefusalText(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "registry.jsonl.archive", "0002-rotation.jsonl")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte("old\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	err := ensureArchive(archive, []byte("new\n"))
	if err == nil || !strings.Contains(err.Error(), "byte verification failed") || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v, want byte verification refusal text", err)
	}
}

func TestArchiveConsultationProvidesForkParentSessionID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	archDir := filepath.Join(dir, "registry.jsonl.archive")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archDir, "0001-rotation.jsonl"), []byte(`{"kind":"session","guid":"guid-parent","event":"retired","recorded_at":"2026-07-08T00:00:00Z","state":"retired","label":"parent","role":"worker","tool":"codex","sids":[{"sid":"sess-parent","observed_at":"2026-07-08T00:00:00Z","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sess-parent"}}`+"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"kind":"session","guid":"guid-child","event":"registered","recorded_at":"2026-07-08T00:00:01Z","state":"unseated","label":"child","role":"worker","tool":"codex","lineage":{"forked_from":"guid-parent"},"provenance":{"mechanism":"fork","forked_from":"guid-parent"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := LoadWithArchives(path)
	if err != nil {
		t.Fatal(err)
	}
	parent := Resolve(recs, "guid-parent")
	if parent == nil || !parent.Archived {
		t.Fatalf("parent = %+v, want archive-resolved parent", parent)
	}
	if sid := ToolSessionIDForGUID(recs, "guid-parent"); sid != "sess-parent" {
		t.Fatalf("parent sid = %q, want sess-parent", sid)
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
	mustAppendLegacySessionEvent(t, path, mustMarshalRecord(t, recognised), "recognised", v2.StateSeated)
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
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
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
		t.Fatalf("latest seated view = %+v, want registered seat with carried hcom_name and fresh spawn coordinates", latest)
	}
	if latest.HcomVerified == nil || *latest.HcomVerified {
		t.Fatalf("latest HcomVerified = %v, want explicit false after carry-forward", latest.HcomVerified)
	}
	collapsed := LatestByGUID(recs)
	if len(collapsed) != 1 || collapsed[0].HcomName != "worker-rive" {
		t.Fatalf("LatestByGUID = %+v, want bus-reachable hcom_name", collapsed)
	}
}

func TestRegisteredReplacementNameWithoutProofDefaultsUnverified(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	verified := true
	current := v2.SessionRecord{
		GUID: "guid-replace-name", Event: "recognised", State: v2.StateSeated,
		Seat: &v2.Seat{Kind: "herdr", HcomName: "old-name", HcomVerified: &verified},
	}
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{current}, nil
	}); err != nil {
		t.Fatal(err)
	}
	patch := current
	patch.Event = "registered"
	patch.Seat = &v2.Seat{Kind: "herdr", HcomName: "new-name"}
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{patch}, nil
	}); err != nil {
		t.Fatal(err)
	}
	latest := V2ByGUID(loadProjection(t, path), current.GUID)
	if latest == nil || latest.Seat == nil || latest.Seat.HcomName != "new-name" || latest.Seat.HcomVerified == nil || *latest.Seat.HcomVerified {
		t.Fatalf("latest seat = %+v, want replacement name explicitly unverified", latest)
	}
}

func TestRegisteredCarryForwardStampsFreshNodeAfterCloneRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	guid, label := "guid-clone-registered", "worker"
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
	mustAppendLegacySessionEvent(t, path, mustMarshalRecord(t, recognised), "recognised", v2.StateSeated)
	oldNode := strings.TrimSpace(string(mustReadFile(t, NodeMarkerPath(path))))
	if _, err := InitNode(path, true); err != nil {
		t.Fatal(err)
	}
	newNode := strings.TrimSpace(string(mustReadFile(t, NodeMarkerPath(path))))
	if newNode == oldNode {
		t.Fatalf("clone repair marker = old node %q, want fresh", oldNode)
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
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{V2FromRecord(registered, "registered", v2.StateSeated, "2026-07-08T00:00:01Z")}, nil
	}); err != nil {
		t.Fatal(err)
	}
	latest := V2ByGUID(loadProjection(t, path), guid)
	if latest == nil {
		t.Fatal("missing latest registered row")
	}
	if latest.Node != newNode || latest.Seat == nil || latest.Seat.Node != newNode {
		t.Fatalf("latest = %+v, want fresh node %q on row and seat", latest, newNode)
	}
	if latest.Seat.HcomName != "worker-rive" || latest.Seat.TerminalID != "term_spawn" || latest.Seat.PaneID != "p_spawn" {
		t.Fatalf("latest seat = %+v, want carried hcom_name plus fresh spawn coordinates", latest.Seat)
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
	mustAppendLegacySessionEvent(t, path, mustMarshalRecord(t, recognised), "recognised", v2.StateSeated)
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
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
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

func TestRegisteredCarryMarksUnverifiedThenNoOps(t *testing.T) {
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
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{row}, nil
	}); err != nil {
		t.Fatal(err)
	}
	repeat := row
	repeat.RecordedAt = "2026-07-08T00:00:01Z"
	repeat.Seat = cloneSeat(row.Seat)
	repeat.Seat.HcomName = ""
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{repeat}, nil
	}); err != nil {
		t.Fatal(err)
	}
	latest := V2ByGUID(loadProjection(t, path), guid)
	if latest == nil || latest.Seat == nil || latest.Seat.HcomVerified == nil || *latest.Seat.HcomVerified {
		t.Fatalf("latest = %+v, want carried bus name explicitly unverified", latest)
	}
	before := registryRowCount(t, path)
	repeat.RecordedAt = "2026-07-08T00:00:02Z"
	if err := updateLockedForTest(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{repeat}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if after := registryRowCount(t, path); after != before {
		t.Fatalf("row count = %d, want unchanged %d after repeated unverified carry", after, before)
	}
}

func TestAppendLegacyRetiredPreservesCloseReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	row := []byte(`{"guid":"guid-launch","short_guid":"launch","label":"launch","role":"worker","agent":"codex","terminal_id":"term_L","pane_id":"p_l","hcom_dir":"/hcom","hcom_name":"launch-bus","status":"closed","close_result":"launch_failed","close_reason":"pane exited before lifecycle bind"}`)
	mustAppendLegacySessionEvent(t, path, row, "retired", v2.StateRetired)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want node row plus retired row: %s", len(lines), data)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &got); err != nil {
		t.Fatal(err)
	}
	if got["state"] != v2.StateRetired || got["event"] != "retired" || got["close_result"] != "launch_failed" || got["close_reason"] != "pane exited before lifecycle bind" {
		t.Fatalf("row = %s, want retired launch_failed with close_reason", data)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].State != v2.StateRetired || recs[0].Status != "" || recs[0].CloseResult != "launch_failed" || recs[0].CloseReason == "" {
		t.Fatalf("four-state view = %+v, want retired launch_failed", recs)
	}
}

func TestLockedValidatorPreservesRenameAgainstStaleEnrichment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	guid, oldLabel := "guid-stale", "old"
	if err := Append(path, []byte(`{"guid":"`+guid+`","label":"`+oldLabel+`","role":"worker","agent":"codex","pane_id":"p_old","terminal_id":"term_old","status":"active"}`)); err != nil {
		t.Fatal(err)
	}
	stale := Record{GUID: &guid, Label: &oldLabel, Role: "worker", Agent: "codex", PaneID: "p_new", TerminalID: "term_new", HcomName: "bus-new", Status: "active"}
	if err := updateLockedForTest(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
		current := V2ByGUID(tx.Projection, guid)
		next := *current
		next.Event = "labelled"
		next.Label = "new"
		return []v2.SessionRecord{next}, nil
	}); err != nil {
		t.Fatal(err)
	}
	mustAppendLegacySessionEvent(t, path, mustMarshalRecord(t, stale), "recognised", v2.StateSeated)
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
	if err := updateLockedForTest(path, func(tx LockedUpdate) ([]v2.SessionRecord, error) {
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
	mustAppendLegacySessionEvent(t, path, mustMarshalRecord(t, stale), "recognised", v2.StateSeated)
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

func loadProjection(t *testing.T, path string) *v2.Projection {
	t.Helper()
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return proj
}

func copyRegistryFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("..", "..", "tests", "fixtures", "registry-v2", name)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustMigrationArchivePath(t *testing.T, path string) string {
	t.Helper()
	archive, err := migrationArchivePath(path)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fileMode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func registryArchivePathsForTest(t *testing.T, path string) []string {
	t.Helper()
	paths, err := registryArchivePaths(path)
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func mustMarshalRecord(t *testing.T, rec Record) []byte {
	t.Helper()
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustAppendLegacySessionEvent(t *testing.T, path string, row []byte, event, state string) WriteOutcome {
	t.Helper()
	outcome, err := AppendLegacySessionEvent(path, row, event, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := outcome.Err(); err != nil {
		t.Fatal(err)
	}
	return outcome
}

func updateLockedForTest(path string, fn LockedUpdateFunc) error {
	outcomes, err := UpdateLocked(path, fn)
	if err != nil {
		return err
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			return err
		}
	}
	return nil
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

func TestV2LabelOwnerIgnoresRetiredLabelHolder(t *testing.T) {
	proj, err := v2.Load(strings.NewReader(strings.Join([]string{
		`{"kind":"node","event":"node_registered","node_id":"node-1","recorded_at":"2026-07-08T00:00:00Z"}`,
		`{"guid":"guid-retired","event":"retired","node":"node-1","state":"retired","label":"shared"}`,
		`{"guid":"guid-lost","event":"lost","node":"node-1","state":"lost","label":"gone"}`,
		`{"guid":"guid-unseated","event":"registered","node":"node-1","state":"unseated","label":"live"}`,
	}, "\n")+"\n"), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if owner := V2LabelOwner(proj, "shared", ""); owner != nil {
		t.Fatalf("retired label owner = %+v, want nil", owner)
	}
	if owner := V2LabelOwner(proj, "gone", ""); owner != nil {
		t.Fatalf("lost label owner = %+v, want nil", owner)
	}
	if owner := V2LabelOwner(proj, "live", ""); owner == nil || owner.GUID != "guid-unseated" {
		t.Fatalf("live label owner = %+v, want guid-unseated", owner)
	}
}

func TestV2ResolveMatchesPaneID(t *testing.T) {
	proj, err := v2.Load(strings.NewReader(strings.Join([]string{
		`{"kind":"node","event":"node_registered","node_id":"node-1","recorded_at":"2026-07-08T00:00:00Z"}`,
		`{"guid":"guid-pane","event":"registered","node":"node-1","state":"seated","label":"pane-agent","seat":{"kind":"herdr","pane_id":"p_123"}}`,
	}, "\n")+"\n"), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := V2Resolve(proj, "p_123"); got == nil || got.GUID != "guid-pane" {
		t.Fatalf("V2Resolve pane = %+v, want guid-pane", got)
	}
}
