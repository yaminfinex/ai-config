package hcomidentity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecodeStoppedUsesRealCulledAgentShape(t *testing.T) {
	raw, err := os.ReadFile("testdata/real-stopped-codex.txt")
	if err != nil {
		t.Fatal(err)
	}
	row, err := DecodeStopped("finishedtabsprobe-nelo", raw)
	if err != nil {
		t.Fatal(err)
	}
	if row.Name != "finishedtabsprobe-nelo" || row.BaseName != "nelo" || row.Tool != "codex" || row.Status != "retired" || row.SessionID != "01a042e5-df2c-76f2-a4f1-66ba11ddc1bb" || row.TranscriptPath == "" {
		t.Fatalf("stopped row = %#v", row)
	}
}

func TestDecodeStoppedDistinguishesMissingAndMalformedEvidence(t *testing.T) {
	if _, err := DecodeStopped("missing", []byte("No stopped events found for 'missing'\n")); !errors.Is(err, ErrStoppedNotFound) {
		t.Fatalf("missing error = %v", err)
	}
	if _, err := DecodeStopped("dore", []byte("Stopped: dore\n  Tool: codex\n")); err == nil {
		t.Fatal("incomplete stopped record was accepted")
	}
}

func TestDecodeStoppedCarriesValidatedSubagentTranscriptEvidence(t *testing.T) {
	const parentSessionID = "c28b3424-9baf-4808-a7a2-a728a5340bac"
	const agentID = "a49beeb7f81d46586"
	raw := []byte("Stopped: raza_general_purpose_1\n" +
		"  Time: 2026-08-31T04:28:04Z\n" +
		"  Tool: claude\n" +
		"  Transcript: /fixture/projects/slug/" + parentSessionID + "/subagents/agent-" + agentID + ".jsonl\n")
	row, err := DecodeStopped("review-raza_general_purpose_1", raw)
	if err != nil {
		t.Fatal(err)
	}
	if row.AgentID != agentID || row.ParentSessionID != parentSessionID {
		t.Fatalf("stopped subagent evidence = %#v", row)
	}

	for _, transcript := range []string{
		"/fixture/projects/slug/not-a-session/subagents/agent-" + agentID + ".jsonl",
		"/fixture/projects/slug/" + parentSessionID + "/subagents/agent-too-short.jsonl",
		"/fixture/projects/slug/" + parentSessionID + "/other/agent-" + agentID + ".jsonl",
	} {
		malformed := []byte("Stopped: child\n  Time: now\n  Tool: claude\n  Transcript: " + transcript + "\n")
		got, decodeErr := DecodeStopped("child", malformed)
		if decodeErr != nil {
			t.Fatalf("shape mismatch should retain ordinary stopped evidence: %v", decodeErr)
		}
		if got.AgentID != "" || got.ParentSessionID != "" {
			t.Errorf("shape mismatch was mis-derived: %#v", got)
		}
	}
}

func TestDecodeArrayAndJSONL(t *testing.T) {
	for name, input := range map[string]string{
		"array": `[{"name":"mavu","tool":"codex","status":"active","launch_context":{"pane_id":"p1"}}]`,
		"jsonl": "{\"name\":\"mavu\",\"tool\":\"codex\",\"status\":\"active\",\"launch_context\":{\"pane_id\":\"p1\"}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			rows, err := Decode([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Name != "mavu" || rows[0].LaunchContext.PaneID != "p1" {
				t.Fatalf("rows = %#v", rows)
			}
		})
	}
}

func TestParentUsesOnlyOneExactBaseName(t *testing.T) {
	child := Row{Name: "probe-parent_general_purpose_1", ParentName: "parent"}
	rows := []Row{{Name: "probe-parent", BaseName: "parent"}, child}
	parent, ok := Parent(rows, child)
	if !ok || parent.Name != "probe-parent" {
		t.Fatalf("Parent() = %#v, %v", parent, ok)
	}

	rows = append(rows, Row{Name: "other-parent", BaseName: "parent"})
	if _, ok := Parent(rows, child); ok {
		t.Fatal("Parent accepted ambiguous base-name identity")
	}
	child.ParentName = "probe-parent"
	if _, ok := Parent(rows, child); ok {
		t.Fatal("Parent inferred from a display name")
	}
}

func TestDecodeEnrichesOnlyProvenParentSession(t *testing.T) {
	rows, err := Decode([]byte(`[
		{"name":"probe-fame","base_name":"fame","session_id":"parent-session","directory":"/probe"},
		{"name":"probe-child","base_name":"child","parent_name":"fame","agent_id":"a35b593a6be7a9ba5"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if rows[1].ParentAgent != "probe-fame" || rows[1].ParentSessionID != "parent-session" || rows[1].ParentDirectory != "/probe" {
		t.Fatalf("enriched child = %#v", rows[1])
	}
}

func TestDecodeParentEnrichmentNeverOverwritesRowOwnedSessionEvidence(t *testing.T) {
	rows, err := Decode([]byte(`[
		{"name":"probe-fame","base_name":"fame","session_id":"live-parent-session","directory":"/live"},
		{"name":"probe-child","base_name":"child","parent_name":"fame","parent_session_id":"carried-parent-session","agent_id":"a35b593a6be7a9ba5"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if rows[1].ParentAgent != "probe-fame" || rows[1].ParentSessionID != "carried-parent-session" {
		t.Fatalf("enrichment overwrote row-owned evidence: %#v", rows[1])
	}
}

func TestDecodeRejectsMalformedRoster(t *testing.T) {
	if _, err := Decode([]byte(`{"name":`)); err == nil {
		t.Fatal("Decode accepted malformed JSON")
	}
}

func TestListContextBoundsHungHcom(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "hcom")
	if err := os.WriteFile(stub, []byte("#!/usr/bin/env bash\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := ListContext(ctx); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("ListContext error = %v", err)
	}
}
