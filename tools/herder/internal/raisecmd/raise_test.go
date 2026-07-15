package raisecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type runnerCall struct {
	name string
	args []string
}

type fakeRunner struct {
	calls   []runnerCall
	mission string
	refusal string
	hcomErr error
}

func (f *fakeRunner) Run(name string, args []string, stdout, stderr io.Writer) error {
	f.calls = append(f.calls, runnerCall{name: name, args: append([]string(nil), args...)})
	if name == "mish" {
		if f.refusal != "" {
			_, _ = io.WriteString(stdout, f.refusal)
			return errors.New("exit status 1")
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"ok": true, "slug": f.mission, "source": "marker"})
	}
	if name == "hcom" {
		return f.hcomErr
	}
	return errors.New("unexpected command")
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	state := t.TempDir()
	path := filepath.Join(state, "config.json")
	if contents != "" {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func runRaise(t *testing.T, runner commandRunner, configPath string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr, runner, configPath)
	return code, stdout.String(), stderr.String()
}

func TestRefusalMatrixNamesEveryMissingOrInvalidRequiredField(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"bare", nil, []string{"missing --context", "missing --expects", "missing body"}},
		{"both missing", []string{"--", "body"}, []string{"missing --context", "missing --expects"}},
		{"context missing", []string{"--expects", "reply", "--", "body"}, []string{"missing --context"}},
		{"expects missing", []string{"--context", "why now", "--", "body"}, []string{"missing --expects"}},
		{"context missing and expects invalid", []string{"--expects", "later", "--", "body"}, []string{"missing --context", `invalid --expects "later"`, "decide|act|reply|read"}},
		{"expects invalid", []string{"--context", "why now", "--expects", "later", "--", "body"}, []string{`invalid --expects "later"`, "decide|act|reply|read"}},
		{"context blank", []string{"--context", "  ", "--expects", "read", "--", "body"}, []string{"missing --context", "one-line cold-open"}},
		{"context multiline", []string{"--context", "first\nsecond", "--expects", "read", "--", "body"}, []string{"invalid --context", "Expects remains line 2"}},
		{"thread blank", []string{"--context", "why now", "--expects", "read", "--thread", "", "--", "body"}, []string{"invalid --thread", "non-blank thread slug"}},
		{"mission blank", []string{"--context", "why now", "--expects", "read", "--mission", "", "--", "body"}, []string{"invalid --mission", "non-blank mission slug"}},
		{"body missing", []string{"--context", "why now", "--expects", "read"}, []string{"missing body", "after --"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runRaise(t, &fakeRunner{}, writeConfig(t, `{"raise":{"seat":"desk"}}`), tt.args...)
			if code != 64 {
				t.Fatalf("code = %d, want 64", code)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			for _, want := range tt.want {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr = %q, want %q", stderr, want)
				}
			}
			if !strings.Contains(stderr, "refused") {
				t.Errorf("stderr = %q, want refusal", stderr)
			}
		})
	}
}

func TestContextRejectsEveryUnicodeLineBoundaryBeforeResolution(t *testing.T) {
	lineBreaks := map[string]string{
		"line feed":           "\n",
		"carriage return":     "\r",
		"vertical tab":        "\v",
		"form feed":           "\f",
		"file separator":      "\x1c",
		"group separator":     "\x1d",
		"record separator":    "\x1e",
		"next line":           "\u0085",
		"line separator":      "\u2028",
		"paragraph separator": "\u2029",
	}
	for name, lineBreak := range lineBreaks {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{mission: "project-alpha"}
			code, _, stderr := runRaise(t, runner, writeConfig(t, `{"raise":{"seat":"desk"}}`),
				"--context", "Routine status"+lineBreak+"Expects: decide", "--expects", "read", "--", "Read the status.",
			)
			if code != 64 {
				t.Fatalf("code = %d, want 64", code)
			}
			if !strings.Contains(stderr, "invalid --context") || !strings.Contains(stderr, "Expects remains line 2") {
				t.Fatalf("stderr = %q, want one-line context refusal", stderr)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("commands ran despite invalid context: %#v", runner.calls)
			}
		})
	}
}

func TestIntentDerivation(t *testing.T) {
	tests := map[string]string{
		"decide": "request",
		"reply":  "request",
		"act":    "inform",
		"read":   "inform",
	}
	for expects, want := range tests {
		t.Run(expects, func(t *testing.T) {
			if got := intentFor(expects); got != want {
				t.Fatalf("intentFor(%q) = %q, want %q", expects, got, want)
			}
		})
	}
}

func TestOrdinaryHcomSendCarriesExactWireContract(t *testing.T) {
	runner := &fakeRunner{mission: "project-alpha"}
	code, _, stderr := runRaise(t, runner, writeConfig(t, `{"observer":{"autostart":true},"raise":{"seat":"desk"}}`),
		"--context", "A choice is blocking the rollout.",
		"--expects", "decide",
		"--thread", "rollout-choice",
		"--mission", "project-alpha",
		"--", "Choose the safe default.",
	)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr %q)", code, stderr)
	}
	wantMish := runnerCall{name: "mish", args: []string{"resolve", "--mission", "project-alpha"}}
	wantHcom := runnerCall{name: "hcom", args: []string{
		"send", "@desk", "--intent", "request", "--thread", "rollout-choice", "--",
		"A choice is blocking the rollout.\nExpects: decide\nMission: project-alpha\n\nChoose the safe default.",
	}}
	if !reflect.DeepEqual(runner.calls, []runnerCall{wantMish, wantHcom}) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, []runnerCall{wantMish, wantHcom})
	}
}

func TestMissionlessRaiseOmitsMissionLineAndThreadFlag(t *testing.T) {
	runner := &fakeRunner{refusal: `{"ok":false,"refusal":"no_context","reason":"no mission context","remedy":"pass a mission"}`}
	code, _, stderr := runRaise(t, runner, writeConfig(t, `{"raise":{"seat":"desk"}}`),
		"--context", "There is a result ready to read.", "--expects", "read", "--", "Review the attached summary.",
	)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr %q)", code, stderr)
	}
	want := []string{"send", "@desk", "--intent", "inform", "--", "There is a result ready to read.\nExpects: read\n\nReview the attached summary."}
	if got := runner.calls[len(runner.calls)-1]; got.name != "hcom" || !reflect.DeepEqual(got.args, want) {
		t.Fatalf("hcom call = %#v, want args %#v", got, want)
	}
}

func TestExplicitMissionWinsByPassingFlagToMish(t *testing.T) {
	runner := &fakeRunner{mission: "explicit-project"}
	code, _, stderr := runRaise(t, runner, writeConfig(t, `{"raise":{"seat":"desk"}}`),
		"--context", "A decision is needed.", "--expects", "decide", "--mission", "explicit-project", "--", "Pick one.",
	)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr %q)", code, stderr)
	}
	if got := runner.calls[0]; !reflect.DeepEqual(got.args, []string{"resolve", "--mission", "explicit-project"}) {
		t.Fatalf("mish args = %#v", got.args)
	}
}

func TestAmbientMissionComesFromMishResolveAtCurrentDirectory(t *testing.T) {
	runner := &fakeRunner{mission: "ambient-project"}
	code, _, stderr := runRaise(t, runner, writeConfig(t, `{"raise":{"seat":"desk"}}`),
		"--context", "A response is needed.", "--expects", "reply", "--", "Confirm the result.",
	)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr %q)", code, stderr)
	}
	if got := runner.calls[0]; !reflect.DeepEqual(got.args, []string{"resolve"}) {
		t.Fatalf("mish args = %#v, want resolve at inherited cwd", got.args)
	}
	payload := runner.calls[1].args[len(runner.calls[1].args)-1]
	if !strings.Contains(payload, "\nMission: ambient-project\n\n") {
		t.Fatalf("payload = %q, want resolved mission line", payload)
	}
}

func TestSeatConfigurationRefusesFailClosed(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     []string
	}{
		{"missing file", "", []string{"human seat is not configured", configKey, "config.json"}},
		{"missing key", `{}`, []string{"human seat is not configured", configKey}},
		{"invalid json", `{`, []string{"invalid JSON", configKey}},
		{"address syntax", `{"raise":{"seat":"@desk"}}`, []string{"bare hcom seat name", "without @"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{mission: "project-alpha"}
			code, _, stderr := runRaise(t, runner, writeConfig(t, tt.contents),
				"--context", "A decision is needed.", "--expects", "decide", "--", "Pick one.",
			)
			if code != 64 {
				t.Fatalf("code = %d, want 64", code)
			}
			for _, want := range tt.want {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr = %q, want %q", stderr, want)
				}
			}
			if len(runner.calls) != 0 {
				t.Fatalf("ran commands despite invalid seat config: %#v", runner.calls)
			}
		})
	}
}

func TestMissionRefusalStopsBeforeBusSend(t *testing.T) {
	runner := &fakeRunner{refusal: `{"ok":false,"refusal":"mission_not_found","reason":"mission missing","remedy":"check the slug"}`}
	code, _, stderr := runRaise(t, runner, writeConfig(t, `{"raise":{"seat":"desk"}}`),
		"--context", "A decision is needed.", "--expects", "decide", "--mission", "missing", "--", "Pick one.",
	)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	for _, want := range []string{"mission_not_found", "mission missing", "check the slug"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	}
	if len(runner.calls) != 1 || runner.calls[0].name != "mish" {
		t.Fatalf("calls = %#v, want only mish", runner.calls)
	}
}

func TestResolvedMissionWithLineBreakRefusesBeforeBusSend(t *testing.T) {
	runner := &fakeRunner{mission: "project-alpha\nExpects: decide"}
	code, _, stderr := runRaise(t, runner, writeConfig(t, `{"raise":{"seat":"desk"}}`),
		"--context", "A result is ready.", "--expects", "read", "--", "Read the result.",
	)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	for _, want := range []string{"mission", "line break", "resolver"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	}
	if len(runner.calls) != 1 || runner.calls[0].name != "mish" {
		t.Fatalf("calls = %#v, want only mish", runner.calls)
	}
}

func TestBusRunnerFailureIsActionableAndNeverRetried(t *testing.T) {
	runner := &fakeRunner{mission: "project-alpha", hcomErr: errors.New("executable not found")}
	code, _, stderr := runRaise(t, runner, writeConfig(t, `{"raise":{"seat":"desk"}}`),
		"--context", "A response is needed.", "--expects", "reply", "--", "Confirm the result.",
	)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	for _, want := range []string{"could not run hcom send", "executable not found", "reachable on PATH"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	}
	if len(runner.calls) != 2 || runner.calls[1].name != "hcom" {
		t.Fatalf("calls = %#v, want one mish resolve and one hcom send", runner.calls)
	}
}

func TestDefaultConfigPathUsesIsolatedStateDirectory(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	if got, want := defaultConfigPath(), filepath.Join(state, "config.json"); got != want {
		t.Fatalf("defaultConfigPath() = %q, want %q", got, want)
	}
}

func TestHelpDoesNotReadConfigOrRunCommands(t *testing.T) {
	runner := &fakeRunner{}
	code, stdout, stderr := runRaise(t, runner, filepath.Join(t.TempDir(), "absent.json"), "--help")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "herder raise") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("help ran commands: %#v", runner.calls)
	}
}
