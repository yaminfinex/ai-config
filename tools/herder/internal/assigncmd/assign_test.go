package assigncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/agentstore"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestAssignWritesExactlyOneEvent(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	code, stdout, stderr := run(t, "impl-geni", "--manager", "ziru", "--group", "fleet-refit", "--by", "owner", "--by-kind", "user")
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "id=") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	data, err := os.ReadFile(filepath.Join(state, "agents", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte("\n")) != 1 {
		t.Fatalf("journal=%s", data)
	}
	var event agentstore.Event
	if err := json.Unmarshal(bytes.TrimSpace(data), &event); err != nil {
		t.Fatal(err)
	}
	if event.Kind != agentstore.KindAssign || event.Name != "impl-geni" || event.Manager != "ziru" || event.Group != "fleet-refit" || event.By != "owner" || event.ByKind != "user" {
		t.Fatalf("event=%+v", event)
	}
}

func TestAssignHumanAndJSON(t *testing.T) {
	t.Setenv("HERDER_STATE_DIR", t.TempDir())
	code, stdout, stderr := run(t, "impl-geni", "--manager", "human", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var event agentstore.Event
	if err := json.Unmarshal([]byte(stdout), &event); err != nil || event.Manager != "human" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestAssignUsage(t *testing.T) {
	t.Setenv("HERDER_STATE_DIR", t.TempDir())
	for _, args := range [][]string{{}, {"impl-geni"}, {"impl-geni", "--name", "impl-hine", "--group", "fleet-refit"}, {"impl-geni", "--name=impl-hine", "--group", "fleet-refit"}} {
		code, _, stderr := run(t, args...)
		if code != 2 || !strings.Contains(stderr, "--manager") || !strings.Contains(stderr, "--group") || !strings.Contains(stderr, "--clear-group") {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr)
		}
	}
}

func TestAssignRequiresAgentBeforeFlags(t *testing.T) {
	code, _, stderr := run(t, "--manager", "human")
	if code != 2 || !strings.Contains(stderr, "herder assign: <agent> must come first") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestAssignHelpDocumentsTheContractAndExamples(t *testing.T) {
	code, stdout, stderr := run(t, "--help")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"one event", "herder register assign", "Later assignment events win by event time", "Exit 0",
		"  herder assign impl-geni --manager ziru\n",
		"  herder assign impl-geni --manager human\n",
		"  herder assign impl-geni --group fleet-refit\n",
		"  herder assign impl-geni --clear-group\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}

func TestAssignStoreUnavailable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_STATE_DIR", file)
	code, _, stderr := run(t, "impl-geni", "--group", "fleet-refit")
	if code != 3 || !strings.Contains(stderr, "store unavailable") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}
