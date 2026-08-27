package hcomidentity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
