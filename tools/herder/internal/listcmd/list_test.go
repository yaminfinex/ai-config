package listcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/continuationstate"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/observerstatus"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestReconciledJSONMissionPrecedenceAndInference(t *testing.T) {
	t.Run("explicit wins over marker", func(t *testing.T) {
		root := t.TempDir()
		cwd := filepath.Join(root, "work", "nested")
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "work", ".mission"), []byte("beta\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		missionsRepo := filepath.Join(root, "mission-repo")
		if err := os.MkdirAll(filepath.Join(missionsRepo, "missions", "beta"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("MISSIONS_REPO", missionsRepo)
		rec := registry.Record{
			Mission:    &v2.Mission{Slug: "alpha", Source: "explicit"},
			Provenance: &registry.Provenance{CWD: cwd},
			Raw:        []byte(`{"kind":"session","guid":"guid-explicit","state":"seated","mission":{"slug":"alpha","source":"explicit"}}`),
		}
		mission := renderedMission(t, reconciledJSON(rec, liveIndex{}, nil, observerstatus.Observation{}))
		if mission == nil || mission.Slug != "alpha" || mission.Source != "explicit" {
			t.Fatalf("mission = %+v", mission)
		}
	})

	t.Run("cwd mission directory fallback", func(t *testing.T) {
		root := t.TempDir()
		missionDir := filepath.Join(root, "missions", "alpha")
		cwd := filepath.Join(missionDir, "work")
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(missionDir, "mission.md"), []byte("mission: alpha\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		rec := registry.Record{
			Provenance: &registry.Provenance{CWD: cwd},
			Raw:        []byte(`{"kind":"session","guid":"guid-cwd","state":"seated"}`),
		}
		mission := renderedMission(t, reconciledJSON(rec, liveIndex{}, nil, observerstatus.Observation{}))
		if mission == nil || mission.Slug != "alpha" || mission.Source != "cwd" {
			t.Fatalf("mission = %+v", mission)
		}
	})

	t.Run("marker fallback", func(t *testing.T) {
		root := t.TempDir()
		cwd := filepath.Join(root, "work", "nested")
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "work", ".mission"), []byte("beta\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		missionsRepo := filepath.Join(root, "mission-repo")
		if err := os.MkdirAll(filepath.Join(missionsRepo, "missions", "beta"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("MISSIONS_REPO", missionsRepo)
		rec := registry.Record{
			Provenance: &registry.Provenance{CWD: cwd},
			Raw:        []byte(`{"kind":"session","guid":"guid-marker","state":"seated"}`),
		}
		mission := renderedMission(t, reconciledJSON(rec, liveIndex{}, nil, observerstatus.Observation{}))
		if mission == nil || mission.Slug != "beta" || mission.Source != "marker" {
			t.Fatalf("mission = %+v", mission)
		}
	})

	t.Run("no context renders null", func(t *testing.T) {
		rec := registry.Record{
			Provenance: &registry.Provenance{CWD: t.TempDir()},
			Raw:        []byte(`{"kind":"session","guid":"guid-none","state":"seated"}`),
		}
		if mission := renderedMission(t, reconciledJSON(rec, liveIndex{}, nil, observerstatus.Observation{})); mission != nil {
			t.Fatalf("mission = %+v, want nil", mission)
		}
	})
}

func TestMissingTrackerAndPaneEvidenceRendersObservationGap(t *testing.T) {
	rec := registry.Record{TerminalID: "terminal-absent", PaneID: "pane-absent"}
	idx := liveIndex{
		byTerm: map[string]*herdrcli.Agent{}, byPane: map[string]*herdrcli.Agent{}, byName: map[string]*herdrcli.Agent{},
		paneTerms: map[string]bool{}, panePanes: map[string]bool{},
	}
	if got := idx.unmatchedStatus(rec); got != "observation_gap" {
		t.Fatalf("unmatched status = %q, want observation_gap", got)
	}
	idx.paneTerms[rec.TerminalID] = true
	if got := idx.unmatchedStatus(rec); got != "undetected" {
		t.Fatalf("pane-present tracker gap status = %q, want undetected", got)
	}
}

func TestPossiblePaneHuskAdviceIsOperatorActionable(t *testing.T) {
	got := observerAdviceSuffix([]observerstatus.Flag{{Type: "possible-pane-husk"}})
	want := " [observer advice: possible pane husk; inspect, then cull deliberately]"
	if got != want {
		t.Fatalf("advice suffix = %q, want %q", got, want)
	}
}

func renderedMission(t *testing.T, raw []byte) *v2.Mission {
	t.Helper()
	var row struct {
		Mission *v2.Mission `json:"mission"`
	}
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return row.Mission
}

func TestFailedContinuationIsVisibleAndExplicitlyAcknowledged(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", stateDir)
	rec := continuationstate.Record{
		ID: "compact-then-self-42", Status: "failed", Target: "worker-hone",
		UpdatedAt: "2026-07-12T12:00:00Z", Reason: "turn end never proven",
		LogPath: "/tmp/sender.log", RecoveryCommand: "herder send worker-hone -- 'continue'",
	}
	if err := continuationstate.Write("", rec); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(continuationstate.DefaultDir(), "foreign.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var visible bytes.Buffer
	renderContinuationFailures(&visible, []continuationstate.Record{rec})
	for _, want := range []string{rec.ID, "@worker-hone", rec.Reason, rec.RecoveryCommand, rec.LogPath, "--ack-continuation"} {
		if !strings.Contains(visible.String(), want) {
			t.Fatalf("failure surface missing %q:\n%s", want, visible.String())
		}
	}

	binDir := t.TempDir()
	for _, name := range []string{"herdr", "jq"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	var stdout, stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("list exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "UNRESOLVED DETACHED CONTINUATIONS") || !strings.Contains(stdout.String(), rec.ID) {
		t.Fatalf("routine list surface omitted failure:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "ignoring continuation record") {
		t.Fatalf("list did not warn about skipped foreign record: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--ack-continuation", rec.ID}, &stdout, &stderr); code != 0 {
		t.Fatalf("ack exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "if recovery is still needed") {
		t.Fatalf("ack output dropped recovery remedy: %s", stdout.String())
	}
	failed, warnings, err := continuationstate.Unresolved("")
	if err != nil || len(warnings) != 1 || len(failed) != 0 {
		t.Fatalf("unresolved after ack = %+v, warnings=%v, err=%v; want only foreign-record warning", failed, warnings, err)
	}
}

func TestRemovedTeamsFlagIsUnknown(t *testing.T) {
	var stderr bytes.Buffer
	_, code := parseArgs([]string{"--teams"}, io.Discard, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "unknown arg: --teams") {
		t.Fatalf("parseArgs() code=%d stderr=%q, want unknown-arg refusal", code, stderr.String())
	}
}

func TestUnresolvedContinuationJSONIsDocumentLevel(t *testing.T) {
	failure := continuationstate.Record{ID: "failed", Status: "failed", Target: "target-fixture"}
	var out, stderr bytes.Buffer
	renderJSONContinuationFailures(&out, &stderr, []continuationstate.Record{failure})
	for _, want := range []string{`"kind":"unresolved_continuation"`, `"id":"failed"`, `"target":"target-fixture"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("JSON output missing %s: %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), `"unresolved_continuations"`) {
		t.Fatalf("JSON output retained per-session attachment: %s", out.String())
	}
}

func TestUnresolvedContinuationJSONMarshalFailureIsLoud(t *testing.T) {
	failure := continuationstate.Record{ID: "failed", Status: "failed", Target: "target-fixture"}
	var out, stderr bytes.Buffer
	renderJSONContinuationFailuresWith(&out, &stderr, []continuationstate.Record{failure}, func(any) ([]byte, error) {
		return nil, errors.New("encoder unavailable")
	})
	if out.Len() != 0 {
		t.Fatalf("marshal failure wrote JSON: %s", out.String())
	}
	for _, want := range []string{"failed", "encoder unavailable", "Inspect", "retry"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("marshal warning missing %q: %s", want, stderr.String())
		}
	}
}

func TestContextSnapshotDisplayNormalizesPercent(t *testing.T) {
	rec := writeContextSnapshot(t, "fresh-rive", "CTX_PCT=+42\nCTX_TS=100\n")

	got := contextSnapshotDisplay(rec, time.Unix(100, 0))
	if got != "42%" {
		t.Fatalf("contextSnapshotDisplay = %q, want normalized 42%%", got)
	}
}

func TestContextSnapshotDisplayRejectsHostilePercent(t *testing.T) {
	for _, pct := range []string{"Inf", "1e300", "0x1p10", strings.Repeat("9", 26)} {
		t.Run(pct, func(t *testing.T) {
			rec := writeContextSnapshot(t, "worker-rive", "CTX_PCT="+pct+"\nCTX_TS=100\n")

			got := contextSnapshotDisplay(rec, time.Unix(100, 0))
			if got != "unknown" {
				t.Fatalf("contextSnapshotDisplay(%q) = %q, want unknown", pct, got)
			}
		})
	}
}

func TestContextSnapshotDisplayRejectsFarFutureTimestamp(t *testing.T) {
	rec := writeContextSnapshot(t, "future-rive", "CTX_PCT=24\nCTX_TS=9223372036854775807\n")

	got := contextSnapshotDisplay(rec, time.Unix(100, 0))
	if got != "unknown" {
		t.Fatalf("future contextSnapshotDisplay = %q, want unknown", got)
	}
}

func writeContextSnapshot(t *testing.T, name, content string) registry.Record {
	t.Helper()
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(statusDir, name+".env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return registry.Record{HcomDir: root, HcomName: name}
}
