package registry

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

const carryTestGeneration = "generation-persisted"

func TestSeatedRewriteEventInventoryCarriesUnownedSeatFacts(t *testing.T) {
	verified := true
	current := v2.SessionRecord{
		Kind: v2.KindSession, GUID: "guid-carry", Event: "registered", State: v2.StateSeated,
		Label: "worker", Role: "worker", Tool: "codex",
		Seat: &v2.Seat{
			Kind: "herdr", TerminalID: "terminal-old", PaneID: "pane-old",
			HcomName: "bus-live", HcomVerified: &verified, HooksBound: true,
			TranscriptPath: "/transcripts/live.jsonl", Namespace: "/bus",
			HcomEpoch: "hcom-epoch", HerdrEpoch: "herdr-epoch",
			CredentialGeneration: carryTestGeneration, ConfirmedAt: "2026-07-18T00:00:00Z",
		},
		Mission: &v2.Mission{Slug: "alpha", Source: "explicit"},
	}
	projection := carryTestProjection(t, current)

	canonical := func(event string) v2.SessionRecord {
		return v2.SessionRecord{
			GUID: current.GUID, Event: event, State: v2.StateSeated,
			Seat: &v2.Seat{
				Kind: "herdr", TerminalID: "terminal-next", PaneID: "pane-next",
				HcomName: "bus-live", HcomVerified: &verified, Namespace: "/bus",
			},
		}
	}
	tests := []struct {
		name      string
		candidate func() v2.SessionRecord
	}{
		{name: "seated", candidate: func() v2.SessionRecord { return canonical("seated") }},
		{name: "recognised", candidate: func() v2.SessionRecord { return canonical("recognised") }},
		{name: "reconciled", candidate: func() v2.SessionRecord { return canonical("reconciled") }},
		{name: "registered", candidate: func() v2.SessionRecord { return canonical("registered") }},
		{name: "labelled", candidate: func() v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "labelled", Label: "renamed"}
		}},
		{name: "label_transferred", candidate: func() v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "label_transferred", Label: "transferred"}
		}},
		{name: "mission_joined", candidate: func() v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "mission_joined", Mission: &v2.Mission{Slug: "beta", Source: "explicit"}}
		}},
		{name: "mission_left", candidate: func() v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "mission_left"}
		}},
		{name: "attested_binding", candidate: func() v2.SessionRecord {
			row := canonical(v2.EventAttestedBinding)
			row.Attestations = []v2.Attestation{{
				ID: "attestation-reissue", GUID: current.GUID, Operation: v2.AttestationReissueCredential,
				Statement: "authorize credential rotation", PaneID: "pane-next", ObservedAt: "2026-07-18T00:01:00Z",
			}}
			return row
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row, ok, err := normalizeSessionAppend(projection, tt.candidate())
			if err != nil || !ok {
				t.Fatalf("normalize = ok %v err %v row %+v", ok, err, row)
			}
			assertCarriedSeatFacts(t, row.Seat)
		})
	}
}

func TestCanonicalReseatDoesNotResurrectOwnedCoordinates(t *testing.T) {
	current := v2.SessionRecord{
		GUID: "guid-owned", State: v2.StateSeated,
		Seat: &v2.Seat{Kind: "herdr", TerminalID: "terminal-stale", PaneID: "pane-stale", HcomName: "bus-stale", CredentialGeneration: carryTestGeneration},
	}
	candidate := v2.SessionRecord{GUID: current.GUID, Event: "seated", State: v2.StateSeated, Seat: &v2.Seat{Kind: "herdr"}}
	got := carrySeatedSuccessorFacts(candidate, current)
	if got.Seat == nil || got.Seat.TerminalID != "" || got.Seat.PaneID != "" || got.Seat.HcomName != "" {
		t.Fatalf("canonical re-seat resurrected candidate-owned coordinates: %+v", got.Seat)
	}
	if got.Seat.CredentialGeneration != carryTestGeneration {
		t.Fatalf("credential generation = %q, want carried %q", got.Seat.CredentialGeneration, carryTestGeneration)
	}
}

func TestSeatedNilSeatAppendCannotErasePersistedSeat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	current := v2.SessionRecord{
		GUID: "guid-nil-seat", Event: "seated", State: v2.StateSeated, Label: "worker", Tool: "codex",
		Seat: &v2.Seat{
			Kind: "herdr", TerminalID: "terminal-live", PaneID: "pane-live",
			CredentialGeneration: carryTestGeneration,
		},
	}
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{current}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := SingleOutcome(outcomes); err != nil || outcome.Err() != nil {
		t.Fatalf("seed outcome=%+v err=%v", outcome, err)
	}

	outcomes, err = UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			GUID: current.GUID, Event: "seated", State: v2.StateSeated,
			Label: current.Label, Tool: current.Tool, Seat: nil,
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := SingleOutcome(outcomes); err != nil || outcome.Err() != nil {
		t.Fatalf("nil-seat append outcome=%+v err=%v", outcome, err)
	}

	latest := V2ByGUID(loadProjection(t, path), current.GUID)
	if latest == nil || latest.Seat == nil || latest.Seat.TerminalID != "terminal-live" ||
		latest.Seat.PaneID != "pane-live" || latest.Seat.CredentialGeneration != carryTestGeneration {
		t.Fatalf("nil-seat seated append erased persisted seat facts: %+v", latest)
	}
}

func TestSeatedCarryAllowsOwnedCredentialRotation(t *testing.T) {
	current := v2.SessionRecord{GUID: "guid-rotate", State: v2.StateSeated, Seat: &v2.Seat{Kind: "process", PID: 41, CredentialGeneration: "generation-old"}}
	candidate := v2.SessionRecord{GUID: current.GUID, Event: v2.EventAttestedBinding, State: v2.StateSeated, Seat: &v2.Seat{Kind: "process", PID: 41, CredentialGeneration: "generation-new"}}
	got := carrySeatedSuccessorFacts(candidate, current)
	if got.Seat == nil || got.Seat.CredentialGeneration != "generation-new" {
		t.Fatalf("credential generation = %+v, want nonempty candidate rotation", got.Seat)
	}
}

func TestSeatRewriteWriterPinsDependOnStructuralCarry(t *testing.T) {
	verified := true
	canonicalSeat := func() *v2.Seat {
		return &v2.Seat{
			Kind: "herdr", TerminalID: "terminal-live", PaneID: "pane-live",
			HcomName: "bus-live", HcomVerified: &verified, Namespace: "/bus",
		}
	}
	writers := []struct {
		name   string
		source string
		build  func(v2.SessionRecord) v2.SessionRecord
	}{
		{name: "sidecar enrichment", source: "sidecarcmd/sidecar.go", build: func(current v2.SessionRecord) v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "seated", State: v2.StateSeated, Label: current.Label, Role: current.Role, Tool: current.Tool, Seat: canonicalSeat()}
		}},
		{name: "observer reconfirm", source: "observercmd/observer.go", build: func(current v2.SessionRecord) v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "reconciled", State: v2.StateSeated, Label: current.Label, Role: current.Role, Tool: current.Tool, Seat: canonicalSeat(), ObservedVia: "observer reconfirm"}
		}},
		{name: "grok capability publication", source: "grokbridge/binder.go", build: func(current v2.SessionRecord) v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "registered", State: v2.StateSeated, Label: current.Label, Role: current.Role, Tool: current.Tool, Seat: canonicalSeat(), Capabilities: &v2.Capabilities{Bus: "bound", Wake: "armed"}}
		}},
		{name: "grok completion replay", source: "grokbridge/completion.go", build: func(current v2.SessionRecord) v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "registered", State: v2.StateSeated, Label: current.Label, Role: current.Role, Tool: current.Tool, Seat: canonicalSeat()}
		}},
		{name: "rename", source: "renamecmd/rename.go", build: func(current v2.SessionRecord) v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "labelled", Label: "renamed"}
		}},
		{name: "mission membership", source: "missioncmd/mission.go", build: func(current v2.SessionRecord) v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "mission_joined", Mission: &v2.Mission{Slug: "alpha", Source: "explicit"}}
		}},
		{name: "spawn replay", source: "spawncmd/spawn.go", build: func(current v2.SessionRecord) v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "registered", State: v2.StateSeated, Label: current.Label, Role: current.Role, Tool: current.Tool, Seat: canonicalSeat()}
		}},
		{name: "repair attestation", source: "repaircmd/repair.go", build: func(current v2.SessionRecord) v2.SessionRecord {
			return v2.SessionRecord{
				GUID: current.GUID, Event: v2.EventAttestedBinding, State: v2.StateSeated,
				Label: current.Label, Role: current.Role, Tool: current.Tool, Seat: canonicalSeat(),
				Attestations: []v2.Attestation{{
					ID: "attestation-reissue", GUID: current.GUID, Operation: v2.AttestationReissueCredential,
					Statement: "authorize credential rotation", PaneID: "pane-live", ObservedAt: "2026-07-18T00:01:00Z",
				}},
			}
		}},
		{name: "lifecycle reseat", source: "lifecyclecmd/lifecycle.go", build: func(current v2.SessionRecord) v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "seated", State: v2.StateSeated, Label: current.Label, Role: current.Role, Tool: current.Tool, Seat: canonicalSeat(), ObservedVia: "lifecycle reseat"}
		}},
		{name: "reconcile apply", source: "reconcilecmd/reconcile.go", build: func(current v2.SessionRecord) v2.SessionRecord {
			return v2.SessionRecord{GUID: current.GUID, Event: "seated", State: v2.StateSeated, Label: current.Label, Role: current.Role, Tool: current.Tool, Seat: canonicalSeat(), ObservedVia: "reconcile apply"}
		}},
	}

	for _, writer := range writers {
		t.Run(writer.name+" ["+writer.source+"]", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "registry.jsonl")
			current := v2.SessionRecord{
				GUID: "guid-writer", Event: "seated", State: v2.StateSeated,
				Label: "worker", Role: "worker", Tool: "codex", Seat: canonicalSeat(),
			}
			current.Seat.CredentialGeneration = carryTestGeneration
			outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
				return []v2.SessionRecord{current}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if outcome, err := SingleOutcome(outcomes); err != nil || outcome.Err() != nil {
				t.Fatalf("seed outcome=%+v err=%v", outcome, err)
			}

			candidate := writer.build(current)
			if candidate.Seat != nil && candidate.Seat.CredentialGeneration != "" {
				t.Fatalf("%s pin candidate unexpectedly owns credential generation", writer.source)
			}
			outcomes, err = UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
				return []v2.SessionRecord{candidate}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if outcome, err := SingleOutcome(outcomes); err != nil || outcome.Err() != nil {
				t.Fatalf("writer outcome=%+v err=%v", outcome, err)
			}

			latest := V2ByGUID(loadProjection(t, path), current.GUID)
			if latest == nil || latest.Seat == nil || latest.Seat.CredentialGeneration != carryTestGeneration {
				t.Fatalf("%s stripped non-owned credential generation: %+v", writer.source, latest)
			}
		})
	}
}

func TestV2FromRecordRoundTripsCredentialGeneration(t *testing.T) {
	raw := []byte(`{"kind":"session","guid":"guid-roundtrip","event":"seated","state":"seated","label":"worker","tool":"codex","seat":{"kind":"herdr","terminal_id":"terminal-live","pane_id":"pane-live","credential_generation":"generation-live"}}`)
	row, err := SessionEventFromJSON(raw, "seated", v2.StateSeated)
	if err != nil {
		t.Fatal(err)
	}
	if row.Seat == nil || row.Seat.CredentialGeneration != "generation-live" {
		t.Fatalf("round-tripped seat = %+v", row.Seat)
	}
}

func TestCompatibilityAppendWritersCarryCredentialGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	current := v2.SessionRecord{
		GUID: "guid-compat", Event: "seated", State: v2.StateSeated, Label: "worker", Role: "worker", Tool: "codex",
		Seat: &v2.Seat{Kind: "herdr", TerminalID: "terminal-live", PaneID: "pane-live", CredentialGeneration: carryTestGeneration},
	}
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{current}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := SingleOutcome(outcomes); err != nil || outcome.Err() != nil {
		t.Fatalf("seed outcome=%+v err=%v", outcome, err)
	}

	legacySnapshot := []byte(`{"guid":"guid-compat","label":"worker","role":"worker","agent":"codex","terminal_id":"terminal-live","pane_id":"pane-live","status":"active"}`)
	if err := Append(path, legacySnapshot); err != nil {
		t.Fatal(err)
	}
	assertLatestCredential(t, path, carryTestGeneration)
	if outcome, err := AppendLegacySessionEvent(path, legacySnapshot, "recognised", v2.StateSeated); err != nil || outcome.Err() != nil {
		t.Fatalf("legacy event outcome=%+v err=%v", outcome, err)
	}
	assertLatestCredential(t, path, carryTestGeneration)
}

func carryTestProjection(t *testing.T, row v2.SessionRecord) *v2.Projection {
	t.Helper()
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := v2.Load(bytes.NewReader(append(raw, '\n')), v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func assertCarriedSeatFacts(t *testing.T, seat *v2.Seat) {
	t.Helper()
	if seat == nil || seat.CredentialGeneration != carryTestGeneration || !seat.HooksBound ||
		seat.TranscriptPath != "/transcripts/live.jsonl" || seat.HcomEpoch != "hcom-epoch" || seat.HerdrEpoch != "herdr-epoch" {
		t.Fatalf("seat facts were not carried: %+v", seat)
	}
}

func assertLatestCredential(t *testing.T, path, want string) {
	t.Helper()
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	latest := V2ByGUID(projection, "guid-compat")
	if latest == nil || latest.Seat == nil || latest.Seat.CredentialGeneration != want {
		t.Fatalf("latest compatibility row stripped credential generation: %+v", latest)
	}
}
