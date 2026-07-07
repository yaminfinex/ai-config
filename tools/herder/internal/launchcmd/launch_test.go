package launchcmd

import (
	"strings"
	"testing"

	"ai-config/tools/herder/internal/hookcmd"
)

func TestThreadCodexSubagentsBlock_AddsFlagWhenAbsent(t *testing.T) {
	got := threadCodexSubagentsBlock([]string{"--model", "gpt-5"})
	if len(got) != 4 {
		t.Fatalf("want 4 args, got %d: %v", len(got), got)
	}
	if got[0] != "--model" || got[1] != "gpt-5" {
		t.Errorf("existing args disturbed: %v", got[:2])
	}
	if got[2] != "-c" || got[3] != "developer_instructions="+hookcmd.CodexSubagentsBlock {
		t.Errorf("block not appended as -c developer_instructions=<block>: %v", got[2:])
	}
}

func TestThreadCodexSubagentsBlock_MergesIntoTwoTokenForm(t *testing.T) {
	got := threadCodexSubagentsBlock([]string{"-c", "developer_instructions=USER TEXT", "--model", "gpt-5"})
	if len(got) != 4 {
		t.Fatalf("want 4 args (no second flag), got %d: %v", len(got), got)
	}
	want := "developer_instructions=USER TEXT\n---\n" + hookcmd.CodexSubagentsBlock
	if got[1] != want {
		t.Errorf("block not merged into existing value:\n got %q\nwant %q", got[1], want)
	}
}

func TestThreadCodexSubagentsBlock_MergesIntoEqualsForm(t *testing.T) {
	for _, prefix := range []string{"-c=", "--config="} {
		in := []string{prefix + "developer_instructions=USER TEXT"}
		got := threadCodexSubagentsBlock(in)
		if len(got) != 1 {
			t.Fatalf("%s: want 1 arg, got %d: %v", prefix, len(got), got)
		}
		want := prefix + "developer_instructions=USER TEXT\n---\n" + hookcmd.CodexSubagentsBlock
		if got[0] != want {
			t.Errorf("%s: block not merged:\n got %q\nwant %q", prefix, got[0], want)
		}
	}
}

// hcom keeps only the LAST developer_instructions flag and drops earlier ones,
// so the block must land in the last occurrence to survive.
func TestThreadCodexSubagentsBlock_MergesIntoLastOccurrence(t *testing.T) {
	got := threadCodexSubagentsBlock([]string{
		"-c", "developer_instructions=FIRST",
		"-c", "developer_instructions=LAST",
	})
	if len(got) != 4 {
		t.Fatalf("want 4 args, got %d: %v", len(got), got)
	}
	if got[1] != "developer_instructions=FIRST" {
		t.Errorf("first occurrence should be untouched, got %q", got[1])
	}
	if !strings.HasPrefix(got[3], "developer_instructions=LAST\n---\n## SUBAGENTS") {
		t.Errorf("block not merged into last occurrence, got %q", got[3])
	}
}

// A bare developer_instructions=... token without a -c/--config flag is not a
// codex config entry; it must be left alone and the block added as a new flag.
func TestThreadCodexSubagentsBlock_IgnoresBareToken(t *testing.T) {
	got := threadCodexSubagentsBlock([]string{"developer_instructions=NOT A FLAG"})
	if len(got) != 3 {
		t.Fatalf("want 3 args, got %d: %v", len(got), got)
	}
	if got[0] != "developer_instructions=NOT A FLAG" {
		t.Errorf("bare token disturbed: %q", got[0])
	}
	if got[1] != "-c" || !strings.HasPrefix(got[2], "developer_instructions=## SUBAGENTS") {
		t.Errorf("block not appended as its own flag: %v", got[1:])
	}
}

func TestThreadCodexSubagentsBlock_DoesNotMutateInput(t *testing.T) {
	in := []string{"-c", "developer_instructions=USER TEXT"}
	orig := append([]string(nil), in...)
	_ = threadCodexSubagentsBlock(in)
	for i := range in {
		if in[i] != orig[i] {
			t.Errorf("input slice mutated at %d: %q", i, in[i])
		}
	}
}
