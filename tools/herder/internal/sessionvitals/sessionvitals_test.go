package sessionvitals

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
)

func TestReadSelectsToolReadersAndReportsObservedFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	observed := time.Date(2026, 9, 10, 12, 34, 56, 0, time.UTC)

	claudeID := "73100000-0000-4000-8000-000000000731"
	claudePath := filepath.Join(home, ".claude", "projects", "-invented-violet", claudeID+".jsonl")
	copyFixture(t, filepath.Join("..", "claudesession", "testdata", "vitals.jsonl"), claudePath, observed)

	codexID := "73200000-0000-4000-8000-000000000732"
	codexPath := filepath.Join(home, ".codex", "sessions", "2026", "09", "10", "rollout-invented-"+codexID+".jsonl")
	copyFixture(t, filepath.Join("..", "codexsession", "testdata", "vitals.jsonl"), codexPath, observed)

	parentID := "73300000-0000-4000-8000-000000000733"
	parentPath := filepath.Join(home, ".claude", "projects", "-invented-parent", parentID+".jsonl")
	copyFixture(t, filepath.Join("..", "claudesession", "testdata", "vitals.jsonl"), parentPath, observed)
	agentID := "a35b593a6be7a9ba5"
	childPath := filepath.Join(parentPath[:len(parentPath)-len(".jsonl")], "subagents", "agent-"+agentID+".jsonl")
	child := `{"isSidechain":true,"type":"assistant","message":{"model":"invented-subagent","usage":{"input_tokens":13,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":17}}}` + "\n"
	writeFixture(t, childPath, []byte(child), observed)

	tests := []struct {
		name, model, path string
		used, window      int64
		row               hcomidentity.Row
	}{
		{"claude main", "invented-claude-latest", claudePath, 1121, 0, hcomidentity.Row{Tool: "claude", Directory: "/invented/violet", SessionID: claudeID}},
		{"claude subagent", "invented-subagent", childPath, 1113, 0, hcomidentity.Row{Tool: "claude", AgentID: agentID, ParentSessionID: parentID, ParentDirectory: "/invented/parent"}},
		{"codex", "invented-codex-latest", codexPath, 1009, 7310, hcomidentity.Row{Tool: "codex", SessionID: codexID}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			read, err := Read(tc.row)
			if err != nil {
				t.Fatal(err)
			}
			vitals, path, gotObserved := read.Vitals, read.Path, read.ObservedAt
			if read.Source != "direct" {
				t.Fatalf("source = %q, want direct with no serve", read.Source)
			}
			if vitals.Model != tc.model || vitals.ContextUsage == nil || vitals.ContextUsage.UsedTokens != tc.used || path != tc.path || !gotObserved.Equal(observed) {
				t.Fatalf("Read() = %+v, %q, %s", vitals, path, gotObserved)
			}
			if tc.window > 0 && (vitals.ContextUsage.WindowTokens == nil || *vitals.ContextUsage.WindowTokens != tc.window || vitals.ContextUsage.UsedPercent == nil) {
				t.Fatalf("context denominator missing: %+v", vitals.ContextUsage)
			}
		})
	}
}

func TestReadTreatsUnresolvablePathAsMissingVitals(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	read, err := Read(hcomidentity.Row{Tool: "codex", SessionID: "73400000-0000-4000-8000-000000000734"})
	vitals, path, observed := read.Vitals, read.Path, read.ObservedAt
	if err != nil || vitals.Model != "" || vitals.ContextUsage != nil || path != "" || !observed.IsZero() {
		t.Fatalf("Read() = %+v, %q, %s, %v", vitals, path, observed, err)
	}
}

func TestKilo(t *testing.T) {
	tests := []struct {
		value int64
		want  string
	}{
		{0, "0"},
		{820, "820"},
		{82000, "82k"},
		{82499, "82k"},
		{82500, "83k"},
	}
	for _, tc := range tests {
		if got := Kilo(tc.value); got != tc.want {
			t.Errorf("Kilo(%d) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func copyFixture(t *testing.T, source, target string, observed time.Time) {
	t.Helper()
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, target, raw, observed)
}

func writeFixture(t *testing.T, path string, raw []byte, observed time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, observed, observed); err != nil {
		t.Fatal(err)
	}
}
