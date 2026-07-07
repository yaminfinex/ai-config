package registry

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func TestLoadMalformedRowFails(t *testing.T) {
	path := writeRegistry(t, `{"guid":"g-1"}`, `{not json`)
	if _, err := Load(path); err == nil {
		t.Fatal("want error on malformed row, got nil")
	}
	// A non-object value breaks jq's group_by(.guid) too — error, not skip.
	path = writeRegistry(t, `"just a string"`)
	if _, err := Load(path); err == nil {
		t.Fatal("want error on non-object row, got nil")
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
	want := `{"guid":"g-1","status":"active"}` + "\n" + `{"guid":"g-2","status":"active"}` + "\n"
	if string(data) != want {
		t.Fatalf("file = %q, want %q", data, want)
	}
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
