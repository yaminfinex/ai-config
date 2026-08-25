package webaction

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpawnPreservesArgvAndParsesWrapperOutput(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tools", "fleet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "args")
	stub := filepath.Join(dir, "spawn.sh")
	script := "#!/usr/bin/env bash\nfor arg in \"$@\"; do printf '<%s>\\n' \"$arg\"; done >\"$ACTION_LOG\"\nprintf '%s\\n' 'Started the launch process' 'Names: api-vava' 'Batch id: batch-1' >&2\nprintf '%s\\n' 'name=api-vava' 'pane=w1:p9' 'cwd=/repo' 'placement=split-pane'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_CONFIG_ROOT", root)
	t.Setenv("ACTION_LOG", log)
	prompt := "--review 'quoted'\nsecond line"
	result, err := Spawn(context.Background(), []string{"codex", "--tag", "api", "--split-from", "w1:p1", "--prompt", prompt})
	if err != nil || result != (Result{Name: "api-vava", Pane: "w1:p9"}) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	got, _ := os.ReadFile(log)
	if !strings.Contains(string(got), "<"+prompt+">\n") {
		t.Fatalf("argv=%q", got)
	}
}

func TestSpawnQuotesRefusalAndMissingScriptIsUnavailable(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tools", "fleet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "spawn.sh")
	if err := os.WriteFile(stub, []byte("#!/usr/bin/env bash\nprintf 'fleet spawn: pane is busy\\n' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_CONFIG_ROOT", root)
	if _, err := Spawn(context.Background(), nil); err == nil || errors.Is(err, ErrUnavailable) || err.Error() != "fleet spawn: pane is busy" {
		t.Fatalf("refusal=%v", err)
	}
	if err := os.Remove(stub); err != nil {
		t.Fatal(err)
	}
	if _, err := Spawn(context.Background(), nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing=%v", err)
	}
}

func TestForkPreservesOptionalPromptAndParsesHcomOutput(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	stub := filepath.Join(dir, "hcom")
	script := "#!/usr/bin/env bash\nfor arg in \"$@\"; do printf '<%s>\\n' \"$arg\"; done >\"$ACTION_LOG\"\nprintf '%s\\n' 'Started the fork process for 1 Codex agent' 'Names: fork-vava' 'Batch id: batch-fork'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("ACTION_LOG", log)
	prompt := "--continue 'safely'\nnow"
	name, err := Fork(context.Background(), "dore", prompt, true)
	if err != nil || name != "fork-vava" {
		t.Fatalf("name=%q err=%v", name, err)
	}
	got, _ := os.ReadFile(log)
	want := "<f>\n<dore>\n<--hcom-prompt>\n<" + prompt + ">\n<--go>\n"
	if string(got) != want {
		t.Fatalf("argv=%q want=%q", got, want)
	}
}

func TestSpawnTimeoutIsUnavailable(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tools", "fleet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "spawn.sh")
	if err := os.WriteFile(stub, []byte("#!/usr/bin/env bash\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_CONFIG_ROOT", root)
	previous := commandTimeout
	commandTimeout = 20 * time.Millisecond
	t.Cleanup(func() { commandTimeout = previous })
	if _, err := Spawn(context.Background(), nil); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout = %v", err)
	}
}
