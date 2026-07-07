package launchcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/hookcmd"
)

func TestThreadCodexBootstrapBlock_AddsFlagWhenAbsent(t *testing.T) {
	got := threadCodexBootstrapBlock([]string{"--model", "gpt-5"})
	if len(got) != 4 {
		t.Fatalf("want 4 args, got %d: %v", len(got), got)
	}
	if got[0] != "--model" || got[1] != "gpt-5" {
		t.Errorf("existing args disturbed: %v", got[:2])
	}
	if got[2] != "-c" || got[3] != "developer_instructions="+hookcmd.CodexBootstrapBlock {
		t.Errorf("block not appended as -c developer_instructions=<block>: %v", got[2:])
	}
}

func TestThreadCodexBootstrapBlock_MergesIntoTwoTokenForm(t *testing.T) {
	got := threadCodexBootstrapBlock([]string{"-c", "developer_instructions=USER TEXT", "--model", "gpt-5"})
	if len(got) != 4 {
		t.Fatalf("want 4 args (no second flag), got %d: %v", len(got), got)
	}
	want := "developer_instructions=USER TEXT\n---\n" + hookcmd.CodexBootstrapBlock
	if got[1] != want {
		t.Errorf("block not merged into existing value:\n got %q\nwant %q", got[1], want)
	}
}

func TestThreadCodexBootstrapBlock_MergesIntoEqualsForm(t *testing.T) {
	for _, prefix := range []string{"-c=", "--config="} {
		in := []string{prefix + "developer_instructions=USER TEXT"}
		got := threadCodexBootstrapBlock(in)
		if len(got) != 1 {
			t.Fatalf("%s: want 1 arg, got %d: %v", prefix, len(got), got)
		}
		want := prefix + "developer_instructions=USER TEXT\n---\n" + hookcmd.CodexBootstrapBlock
		if got[0] != want {
			t.Errorf("%s: block not merged:\n got %q\nwant %q", prefix, got[0], want)
		}
	}
}

// hcom keeps only the LAST developer_instructions flag and drops earlier ones,
// so the block must land in the last occurrence to survive.
func TestThreadCodexBootstrapBlock_MergesIntoLastOccurrence(t *testing.T) {
	got := threadCodexBootstrapBlock([]string{
		"-c", "developer_instructions=FIRST",
		"-c", "developer_instructions=LAST",
	})
	if len(got) != 4 {
		t.Fatalf("want 4 args, got %d: %v", len(got), got)
	}
	if got[1] != "developer_instructions=FIRST" {
		t.Errorf("first occurrence should be untouched, got %q", got[1])
	}
	if !strings.HasPrefix(got[3], "developer_instructions=LAST\n---\n[HERDER SESSION ADDENDUM]") {
		t.Errorf("block not merged into last occurrence, got %q", got[3])
	}
}

// A bare developer_instructions=... token without a -c/--config flag is not a
// codex config entry; it must be left alone and the block added as a new flag.
func TestThreadCodexBootstrapBlock_IgnoresBareToken(t *testing.T) {
	got := threadCodexBootstrapBlock([]string{"developer_instructions=NOT A FLAG"})
	if len(got) != 3 {
		t.Fatalf("want 3 args, got %d: %v", len(got), got)
	}
	if got[0] != "developer_instructions=NOT A FLAG" {
		t.Errorf("bare token disturbed: %q", got[0])
	}
	if got[1] != "-c" || !strings.HasPrefix(got[2], "developer_instructions=[HERDER SESSION ADDENDUM]") {
		t.Errorf("block not appended as its own flag: %v", got[1:])
	}
}

func TestThreadCodexBootstrapBlock_DoesNotMutateInput(t *testing.T) {
	in := []string{"-c", "developer_instructions=USER TEXT"}
	orig := append([]string(nil), in...)
	_ = threadCodexBootstrapBlock(in)
	for i := range in {
		if in[i] != orig[i] {
			t.Errorf("input slice mutated at %d: %q", i, in[i])
		}
	}
}

// isPrintInvocation must mirror hcom's print switch exactly (any claude arg
// literally "-p" or "--print") and stay claude-only: codex's -p is --profile,
// and a false positive there would silently unbind a real session from the bus.
func TestIsPrintInvocation(t *testing.T) {
	cases := []struct {
		tool string
		args []string
		want bool
	}{
		{"claude", []string{"-p", "hello"}, true},
		{"claude", []string{"--print", "hello"}, true},
		{"claude", []string{"--model", "opus", "-p"}, true},
		{"claude", []string{"--model", "opus"}, false},
		{"claude", []string{}, false},
		// Only exact tokens trigger, matching hcom's argv scan.
		{"claude", []string{"--print-width", "80"}, false},
		{"claude", []string{"-print"}, false},
		{"claude", []string{"prompt with -p inside"}, false},
		// Other tools never bypass: -p means different things there.
		{"codex", []string{"-p", "myprofile"}, false},
		{"gemini", []string{"--print"}, false},
	}
	for _, c := range cases {
		if got := isPrintInvocation(c.tool, c.args); got != c.want {
			t.Errorf("isPrintInvocation(%q, %v) = %v, want %v", c.tool, c.args, got, c.want)
		}
	}
}

// codexStripsDevInstructions must mirror hcom's strip predicate exactly (any
// arg literally "resume" or "fork"): threading on those paths ships argv hcom
// discards before launch.
func TestCodexStripsDevInstructions(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"fork", "sess-123"}, true},
		{[]string{"fork", "--last"}, true},
		{[]string{"resume", "thread-1", "--model", "gpt-5"}, true},
		{[]string{"--model", "gpt-5"}, false},
		{[]string{}, false},
		// Only exact tokens trigger, matching hcom's `arg == "resume"|"fork"`.
		{[]string{"--resume"}, false},
		{[]string{"forked"}, false},
	}
	for _, c := range cases {
		if got := codexStripsDevInstructions(c.args); got != c.want {
			t.Errorf("codexStripsDevInstructions(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestPinConfigDir_SeedsClaudeConfigOnPin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HCOM_DIR", t.TempDir()) // isolated bus → pin fires
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"real":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	PinConfigDir("claude")

	dir := filepath.Join(home, ".claude")
	if got := os.Getenv("CLAUDE_CONFIG_DIR"); got != dir {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q, want %q", got, dir)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude.json"))
	if err != nil {
		t.Fatalf("seed not written: %v", err)
	}
	if string(data) != `{"real":true}` {
		t.Errorf("seed content = %q, want copy of ~/.claude.json", data)
	}
}

func TestPinConfigDir_SeedNeverOverwritesExistingTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HCOM_DIR", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"real":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"pinned":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	PinConfigDir("claude")

	data, _ := os.ReadFile(filepath.Join(dir, ".claude.json"))
	if string(data) != `{"pinned":true}` {
		t.Errorf("existing target overwritten: %q", data)
	}
}

func TestPinConfigDir_SeedSkippedWhenSourceMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HCOM_DIR", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	PinConfigDir("claude")

	if got := os.Getenv("CLAUDE_CONFIG_DIR"); got != filepath.Join(home, ".claude") {
		t.Fatalf("pin should still fire without a seed source, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", ".claude.json")); err == nil {
		t.Error("seed file created out of nothing")
	}
}

func TestPinConfigDir_NoSeedWhenUserPresetConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HCOM_DIR", t.TempDir())
	preset := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", preset)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"real":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	PinConfigDir("claude")

	if got := os.Getenv("CLAUDE_CONFIG_DIR"); got != preset {
		t.Fatalf("preset CLAUDE_CONFIG_DIR clobbered: %q", got)
	}
	if _, err := os.Stat(filepath.Join(preset, ".claude.json")); err == nil {
		t.Error("seeded into a user-preset config dir")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", ".claude.json")); err == nil {
		t.Error("seeded into unpinned default dir")
	}
}

func TestPinConfigDir_NoopOnGlobalBus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HCOM_DIR", filepath.Join(home, ".hcom"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"real":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	PinConfigDir("claude")

	if got := os.Getenv("CLAUDE_CONFIG_DIR"); got != "" {
		t.Fatalf("global bus must not pin, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", ".claude.json")); err == nil {
		t.Error("global bus must not seed")
	}
}
