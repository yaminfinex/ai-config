package observercmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/continuationstate"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/observerstatus"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestApplyCandidatesRefusalLeavesBatchUnapplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{
			{GUID: "guid-healthy", Label: "healthy", State: v2.StateSeated},
			{GUID: "guid-poison", Label: "poison", State: v2.StateSeated},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertObserverWriteOutcomes(t, outcomes)
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	healthy := registry.V2ByGUID(proj, "guid-healthy")
	if healthy == nil {
		t.Fatal("missing healthy session")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cands := []candidate{
		unseatCandidate(*healthy, time.Now().UTC(), "process gone", "process sweep"),
		{
			kind: "unseat",
			guid: "guid-poison",
			row: v2.SessionRecord{
				GUID:     "guid-poison",
				Event:    "legacy_v1_mapped",
				State:    v2.StateUnseated,
				Label:    "poison",
				Tool:     "claude",
				LegacyV1: true,
			},
		},
	}

	var stderr bytes.Buffer
	summary := applyCandidates(path, cands, &stderr)
	if summary.Refused != len(cands) || summary.Applied != 0 {
		t.Fatalf("summary = %+v, want all candidates refused", summary)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("registry changed after refused observer batch:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestContinuationFailureFindingRequiresExplicitAcknowledgement(t *testing.T) {
	stateDir := t.TempDir()
	rec := continuationstate.Record{
		ID: "compact-then-self-42", Status: "failed", Target: "worker-hone",
		UpdatedAt: "2026-07-12T12:00:00Z", Reason: "delivery budget exhausted",
		LogPath: "/tmp/sender.log", RecoveryCommand: "herder send worker-hone -- 'continue'",
	}
	if err := continuationstate.Write(filepath.Join(stateDir, "continuations"), rec); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	flags := continuationFailureFlags(stateDir, &stderr)
	if len(flags) != 1 || flags[0].Type != "failed-continuation" ||
		!strings.Contains(flags[0].Suggested, "--ack-continuation "+rec.ID) {
		t.Fatalf("flags = %+v; want actionable failed-continuation finding", flags)
	}

	// Merely reading again (the equivalent of an observer restart/sweep) retains
	// the finding; only the explicit acknowledgement mutates the record.
	if again := continuationFailureFlags(stateDir, &stderr); len(again) != 1 {
		t.Fatalf("finding cleared without acknowledgement: %+v", again)
	}
	if _, err := continuationstate.Acknowledge(filepath.Join(stateDir, "continuations"), rec.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if cleared := continuationFailureFlags(stateDir, &stderr); len(cleared) != 0 {
		t.Fatalf("finding after acknowledgement = %+v, want cleared", cleared)
	}
}

func TestReconfirmRefreshesBusIdentityFromLiveCorrelate(t *testing.T) {
	rec := v2.SessionRecord{
		GUID:  "guid-self",
		State: v2.StateSeated,
		Seat:  &v2.Seat{Kind: "herdr", PaneID: "p_old", HcomName: "poisoned-name"},
		SIDs:  []v2.SID{{SID: "sess-live"}},
	}
	joined := true
	bus := busState{available: true, rows: map[string]hcomidentity.Row{
		"live-self": {Name: "live-self", SessionID: "sess-live", Joined: &joined, LaunchContext: hcomidentity.LaunchContext{PaneID: "p_new"}},
	}}

	cand := reconfirmCandidate(rec, herdrcli.Pane{PaneID: "p_new"}, bus, time.Now().UTC())
	if cand.row.Seat == nil || cand.row.Seat.HcomName != "live-self" || cand.row.Seat.HcomVerified == nil || !*cand.row.Seat.HcomVerified {
		t.Fatalf("reconfirmed seat = %+v, want verified live-self", cand.row.Seat)
	}
}

func TestSuccessorCarryMarksBusIdentityUnverifiedWithoutProof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	current := v2.SessionRecord{
		GUID:       "guid-old",
		Event:      "seated",
		State:      v2.StateSeated,
		Seat:       &v2.Seat{Kind: "herdr", PaneID: "p_self", HcomName: "poisoned-name"},
		SIDs:       []v2.SID{{SID: "sess-old"}},
		Provenance: v2.Provenance{Mechanism: "enroll"},
	}
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{current}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertObserverWriteOutcomes(t, outcomes)
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}

	rows, ok := turnoverRowsLocked(proj, current, "sess-new", hcomidentity.Result{Reason: "no live proof"}, time.Now().UTC())
	if !ok || len(rows) != 2 || rows[0].Seat == nil || rows[0].Seat.HcomVerified == nil || *rows[0].Seat.HcomVerified {
		t.Fatalf("turnover rows = %+v, want successor with explicit unverified bus identity", rows)
	}
}

func assertObserverWriteOutcomes(t *testing.T, outcomes []registry.WriteOutcome) {
	t.Helper()
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestObservedSIDUsesPaneCorrelatedBusForSidecarlessSeat(t *testing.T) {
	rec := v2.SessionRecord{
		GUID:       "guid-self",
		State:      v2.StateSeated,
		Seat:       &v2.Seat{Kind: "herdr", TerminalID: "term-missing", PaneID: "pane-self", HcomName: "stale-name"},
		Provenance: v2.Provenance{Mechanism: "enroll"},
	}
	joined := true
	bus := busState{available: true, rows: map[string]hcomidentity.Row{
		"live-self": {
			Name: "live-self", SessionID: "session-new", Joined: &joined,
			LaunchContext: hcomidentity.LaunchContext{PaneID: "pane-self"},
		},
	}}

	if got := observedSID(rec, herdrState{available: true, byTerm: map[string]herdrcli.Pane{}}, bus); got != "session-new" {
		t.Fatalf("observedSID = %q, want pane-correlated session-new", got)
	}
}

func TestDoctrineDeliveryRequiresFullUnmanagedCodexCorrelation(t *testing.T) {
	proj, err := v2.Load(bytes.NewReader(nil), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	baseHD := herdrState{
		available: true,
		byTerm: map[string]herdrcli.Pane{
			"term-1": {PaneID: "pane-1", TerminalID: "term-1", AgentSession: "sid-1"},
		},
		procs: map[string]herdrcli.ProcessInfo{
			"term-1": {Processes: []herdrcli.Process{{PID: 4242, Argv: []string{"/usr/local/bin/codex", "resume"}}}},
		},
	}
	baseBus := busState{available: true, rows: map[string]hcomRow{
		"raw-codex": {
			Name: "raw-codex", Tool: "codex", SessionID: "sid-1", ProcessBound: boolPtr(true),
			LaunchContext: hcomLaunchContext{PaneID: "pane-1", ProcessID: "process-1"},
		},
	}}
	joined := func(row hcomRow) bool { return row.Name == "raw-codex" && row.SessionID == "sid-1" }

	tests := []struct {
		name string
		hd   herdrState
		bus  busState
		join func(hcomRow) bool
		want int
	}{
		{name: "full correlation", hd: baseHD, bus: baseBus, join: joined, want: 1},
		{name: "missing live pane and process", hd: herdrState{available: true, byTerm: map[string]herdrcli.Pane{}, procs: map[string]herdrcli.ProcessInfo{}}, bus: baseBus, join: joined},
		{name: "missing tool session id", hd: withPaneSession(baseHD, ""), bus: baseBus, join: joined},
		{name: "missing joined hcom row", hd: baseHD, bus: baseBus, join: func(hcomRow) bool { return false }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := doctrineCandidates(proj, tc.hd, tc.bus, nil, tc.join)
			if len(got) != tc.want {
				t.Fatalf("doctrineCandidates() = %+v, want %d candidate(s)", got, tc.want)
			}
			sent := 0
			deliverDoctrine(got, map[string]string{}, func(string) bool {
				sent++
				return true
			}, time.Now())
			if sent != tc.want {
				t.Fatalf("delivery count = %d, want %d", sent, tc.want)
			}
		})
	}
}

func TestDoctrineDeliverySuppressesManagedAmbiguousAndRepeatedSessions(t *testing.T) {
	hd := herdrState{
		available: true,
		byTerm: map[string]herdrcli.Pane{
			"term-1": {PaneID: "pane-1", TerminalID: "term-1", AgentSession: "sid-1"},
		},
		procs: map[string]herdrcli.ProcessInfo{
			"term-1": {Processes: []herdrcli.Process{{PID: 4242, Argv: []string{"codex", "resume"}}}},
		},
	}
	row := hcomRow{
		Name: "raw-codex", Tool: "codex", SessionID: "sid-1", ProcessBound: boolPtr(true),
		LaunchContext: hcomLaunchContext{PaneID: "pane-1", ProcessID: "process-1"},
	}
	joined := func(hcomRow) bool { return true }
	empty, err := v2.Load(bytes.NewReader(nil), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first := doctrineCandidates(empty, hd, busState{available: true, rows: map[string]hcomRow{row.Name: row}}, nil, joined)
	if len(first) != 1 {
		t.Fatalf("full correlation produced %d candidates, want 1", len(first))
	}
	receipts := map[string]string{first[0].Token: "already sent"}
	if got := doctrineCandidates(empty, hd, busState{available: true, rows: map[string]hcomRow{row.Name: row}}, receipts, joined); len(got) != 0 {
		t.Fatalf("repeat delivery candidates = %+v, want none", got)
	}

	other := row
	other.Name = "other-codex"
	other.LaunchContext.ProcessID = "process-2"
	if got := doctrineCandidates(empty, hd, busState{available: true, rows: map[string]hcomRow{row.Name: row, other.Name: other}}, nil, joined); len(got) != 0 {
		t.Fatalf("ambiguous correlation candidates = %+v, want none", got)
	}

	managedJSON := `{"kind":"session","guid":"managed-guid","event":"seated","recorded_at":"2026-07-12T00:00:00Z","state":"seated","tool":"codex","seat":{"kind":"herdr","terminal_id":"term-1","pane_id":"pane-1","hcom_name":"managed"},"sids":[{"sid":"sid-1"}]}`
	managed, err := v2.Load(bytes.NewBufferString(managedJSON+"\n"), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := doctrineCandidates(managed, hd, busState{available: true, rows: map[string]hcomRow{row.Name: row}}, nil, joined); len(got) != 0 {
		t.Fatalf("managed correlation candidates = %+v, want none", got)
	}
}

func TestDoctrineReceiptTokenIgnoresCodexProcessOrdering(t *testing.T) {
	proj, err := v2.Load(bytes.NewReader(nil), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hd := herdrState{
		available: true,
		byTerm: map[string]herdrcli.Pane{
			"term-1": {PaneID: "pane-1", TerminalID: "term-1", AgentSession: "sid-1"},
		},
		procs: map[string]herdrcli.ProcessInfo{
			"term-1": {Processes: []herdrcli.Process{
				{PID: 1111, Argv: []string{"codex", "resume"}},
				{PID: 2222, Argv: []string{"codex", "resume"}},
			}},
		},
	}
	bus := busState{available: true, rows: map[string]hcomRow{
		"raw-codex": {
			Name: "raw-codex", Tool: "codex", SessionID: "sid-1", ProcessBound: boolPtr(true),
			LaunchContext: hcomLaunchContext{PaneID: "pane-1", ProcessID: "process-1"},
		},
	}}
	first := doctrineCandidates(proj, hd, bus, nil, func(hcomRow) bool { return true })
	hd.procs["term-1"] = herdrcli.ProcessInfo{Processes: []herdrcli.Process{
		{PID: 2222, Argv: []string{"codex", "resume"}},
		{PID: 1111, Argv: []string{"codex", "resume"}},
	}}
	second := doctrineCandidates(proj, hd, bus, nil, func(hcomRow) bool { return true })
	if len(first) != 1 || len(second) != 1 || first[0].Token != "process-1:sid-1" || second[0].Token != first[0].Token {
		t.Fatalf("tokens before/after process reorder = %+v / %+v, want stable process-1:sid-1", first, second)
	}
}

func TestPriorDoctrineDeliveriesRequiresPositiveEvidenceOrRetentionExpiry(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	status := observerstatus.Status{DoctrineDeliveries: map[string]string{
		"live-process:sid-live":          "2026-07-10T00:00:00Z",
		"replaced-process:sid-old":       "2026-07-12T11:59:00Z",
		"old-process:sid-moved":          "2026-07-12T11:59:00Z",
		"recent-process:sid-unconfirmed": "2026-07-12T11:59:00Z",
		"unconfirmed-process:sid-gone":   "2026-07-10T00:00:00Z",
	}}
	if err := observerstatus.WriteAtomic(observerstatus.PathForStateDir(stateDir), status); err != nil {
		t.Fatal(err)
	}
	hd := herdrState{
		available: true,
		byTerm: map[string]herdrcli.Pane{
			"term-live": {PaneID: "pane-live", TerminalID: "term-live", AgentSession: "sid-live"},
		},
		procs: map[string]herdrcli.ProcessInfo{}, // Per-pane process_info failed this sweep.
	}
	bus := busState{available: true, rows: map[string]hcomRow{
		"replacement": {
			Name: "replacement", Tool: "codex", SessionID: "sid-new", ProcessBound: boolPtr(true),
			LaunchContext: hcomLaunchContext{PaneID: "pane-replaced", ProcessID: "replaced-process"},
		},
		"moved": {
			Name: "moved", Tool: "codex", SessionID: "sid-moved", ProcessBound: boolPtr(true),
			LaunchContext: hcomLaunchContext{PaneID: "pane-moved", ProcessID: "new-process"},
		},
	}}
	pruned := priorDoctrineDeliveries(stateDir, hd, bus, now)
	if len(pruned) != 2 || pruned["live-process:sid-live"] == "" || pruned["recent-process:sid-unconfirmed"] == "" {
		t.Fatalf("pruned receipts = %v, want live and recently unconfirmed receipts preserved", pruned)
	}
	preserved := priorDoctrineDeliveries(stateDir, herdrState{}, busState{}, now)
	if len(preserved) != 5 {
		t.Fatalf("receipts during whole-transport gap = %v, want all preserved", preserved)
	}
}

func TestJoinedHcomRowAcceptsObjectAndSingletonArray(t *testing.T) {
	binDir := t.TempDir()
	stub := "#!/usr/bin/env bash\nprintf '%s' \"$HCOM_LIST_JSON\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	want := hcomRow{Name: "raw-codex", Tool: "codex", SessionID: "sid-1"}
	for _, tc := range []struct {
		name string
		json string
		want bool
	}{
		{name: "object", json: `{"name":"raw-codex","tool":"codex","session_id":"sid-1"}`, want: true},
		{name: "singleton array", json: `[{"name":"raw-codex","tool":"codex","session_id":"sid-1"}]`, want: true},
		{name: "ambiguous array", json: `[{"tool":"codex","session_id":"sid-1"},{"tool":"codex","session_id":"sid-1"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HCOM_LIST_JSON", tc.json)
			if got := joinedHcomRow(want); got != tc.want {
				t.Fatalf("joinedHcomRow() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestDoctrineDeliveryRecordsReceiptWithoutRegistryWrite(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	before := []byte("registry sentinel\n")
	if err := os.WriteFile(registryPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	receipts := map[string]string{}
	sent := 0
	deliverDoctrine([]doctrineCandidate{{Name: "raw-codex", Token: "incarnation"}}, receipts, func(name string) bool {
		sent++
		return name == "raw-codex"
	}, time.Date(2026, 7, 12, 1, 2, 3, 0, time.UTC))
	deliverDoctrine([]doctrineCandidate{{Name: "raw-codex", Token: "incarnation"}}, receipts, func(string) bool {
		sent++
		return true
	}, time.Date(2026, 7, 12, 1, 2, 4, 0, time.UTC))
	if sent != 1 || receipts["incarnation"] != "2026-07-12T01:02:03Z" {
		t.Fatalf("sent=%d receipts=%v, want one recorded delivery", sent, receipts)
	}
	after, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("doctrine delivery changed registry:\nbefore=%q\nafter=%q", before, after)
	}
}

func withPaneSession(hd herdrState, sid string) herdrState {
	copyHD := hd
	copyHD.byTerm = map[string]herdrcli.Pane{}
	for term, pane := range hd.byTerm {
		pane.AgentSession = sid
		copyHD.byTerm[term] = pane
	}
	return copyHD
}

func boolPtr(v bool) *bool { return &v }
