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
