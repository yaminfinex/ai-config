// Package raisecmd implements the structured path for opening an item at the
// configured human seat. The transport remains an ordinary hcom message: the
// extra structure rides in the text so consumers can recover the exact
// expectation and mission without a new bus envelope.
package raisecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ai-config/tools/herder/internal/registry"
)

const configKey = "raise.seat"

type options struct {
	context    string
	expects    string
	thread     string
	mission    string
	body       string
	help       bool
	threadSet  bool
	missionSet bool
}

type commandRunner interface {
	Run(name string, args []string, stdout, stderr io.Writer) error
}

type execRunner struct{}

func (execRunner) Run(name string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// Run executes herder raise.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, stdout, stderr, execRunner{}, defaultConfigPath())
}

func run(args []string, stdout, stderr io.Writer, runner commandRunner, configPath string) int {
	opts, problems := parseArgs(args)
	if opts.help {
		printHelp(stdout)
		return 0
	}
	if len(problems) != 0 {
		refuse(stderr, strings.Join(problems, "; "))
		return 64
	}

	seat, err := loadSeat(configPath)
	if err != nil {
		refuse(stderr, err.Error())
		return 64
	}
	mission, err := resolveMission(runner, opts.mission)
	if err != nil {
		refuse(stderr, err.Error())
		return 1
	}

	payload := formatPayload(opts.context, opts.expects, mission, opts.body)
	hcomArgs := []string{"send", "@" + seat, "--intent", intentFor(opts.expects)}
	if opts.thread != "" {
		hcomArgs = append(hcomArgs, "--thread", opts.thread)
	}
	hcomArgs = append(hcomArgs, "--", payload)
	if err := runner.Run("hcom", hcomArgs, stdout, stderr); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			refuse(stderr, fmt.Sprintf("could not run hcom send: %v; verify hcom is installed and reachable on PATH", err))
		}
		return commandExitCode(err)
	}
	return 0
}

func parseArgs(args []string) (options, []string) {
	var opts options
	var problems []string
	seenSeparator := false
	for i := 0; i < len(args); {
		arg := args[i]
		if seenSeparator {
			opts.body = strings.Join(args[i:], " ")
			break
		}
		switch arg {
		case "-h", "--help":
			opts.help = true
			return opts, nil
		case "--":
			seenSeparator = true
			i++
		case "--context", "--expects", "--thread", "--mission":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				problems = append(problems, fmt.Sprintf("%s requires a value; pass %s <value>", arg, arg))
				i++
				continue
			}
			value := args[i+1]
			switch arg {
			case "--context":
				opts.context = value
			case "--expects":
				opts.expects = value
			case "--thread":
				opts.thread = value
				opts.threadSet = true
			case "--mission":
				opts.mission = value
				opts.missionSet = true
			}
			i += 2
		default:
			problems = append(problems, fmt.Sprintf("unknown argument %q; pass flags before -- and the body after --", arg))
			i++
		}
	}

	if strings.TrimSpace(opts.context) == "" {
		problems = append(problems, "missing --context; add a one-line cold-open with --context '<why this is being raised>'")
	} else if strings.ContainsAny(opts.context, "\r\n") {
		problems = append(problems, "invalid --context; put the cold-open on one line so Expects remains line 2")
	}
	if opts.expects == "" {
		problems = append(problems, "missing --expects; pass --expects decide|act|reply|read")
	} else if !validExpects(opts.expects) {
		problems = append(problems, fmt.Sprintf("invalid --expects %q; pass --expects decide|act|reply|read", opts.expects))
	}
	if opts.threadSet && strings.TrimSpace(opts.thread) == "" {
		problems = append(problems, "invalid --thread; pass a non-blank thread slug or omit --thread")
	}
	if opts.missionSet && strings.TrimSpace(opts.mission) == "" {
		problems = append(problems, "invalid --mission; pass a non-blank mission slug or omit --mission")
	}
	if !seenSeparator || strings.TrimSpace(opts.body) == "" {
		problems = append(problems, "missing body; add the item to raise after --")
	}
	return opts, problems
}

func validExpects(value string) bool {
	switch value {
	case "decide", "act", "reply", "read":
		return true
	default:
		return false
	}
}

func intentFor(expects string) string {
	switch expects {
	case "decide", "reply":
		return "request"
	default:
		return "inform"
	}
}

func formatPayload(context, expects, mission, body string) string {
	var b strings.Builder
	b.WriteString(context)
	b.WriteString("\nExpects: ")
	b.WriteString(expects)
	if mission != "" {
		b.WriteString("\nMission: ")
		b.WriteString(mission)
	}
	b.WriteString("\n\n")
	b.WriteString(body)
	return b.String()
}

func defaultConfigPath() string {
	return filepath.Join(filepath.Dir(registry.DefaultPath()), "config.json")
}

func loadSeat(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("human seat is not configured; add {\"raise\":{\"seat\":\"<name>\"}} to %s (%s)", path, configKey)
		}
		return "", fmt.Errorf("cannot read human-seat configuration at %s: %v; make the file readable and set %s", path, err, configKey)
	}
	var cfg struct {
		Raise struct {
			Seat string `json:"seat"`
		} `json:"raise"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return "", fmt.Errorf("human-seat configuration at %s is invalid JSON: %v; fix the file and set %s", path, err, configKey)
	}
	seat := strings.TrimSpace(cfg.Raise.Seat)
	if seat == "" {
		return "", fmt.Errorf("human seat is not configured; add {\"raise\":{\"seat\":\"<name>\"}} to %s (%s)", path, configKey)
	}
	if strings.HasPrefix(seat, "@") || strings.ContainsAny(seat, " \t\r\n") {
		return "", fmt.Errorf("%s in %s must be a bare hcom seat name without @ or whitespace; correct the configured value", configKey, path)
	}
	return seat, nil
}

type missionResult struct {
	OK      bool   `json:"ok"`
	Slug    string `json:"slug"`
	Refusal string `json:"refusal"`
	Reason  string `json:"reason"`
	Remedy  string `json:"remedy"`
}

func resolveMission(runner commandRunner, explicit string) (string, error) {
	args := []string{"resolve"}
	if explicit != "" {
		args = append(args, "--mission", explicit)
	}
	var stdout, stderr bytes.Buffer
	runErr := runner.Run("mish", args, &stdout, &stderr)
	var result missionResult
	decodeErr := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result)
	if runErr == nil && decodeErr == nil && result.OK && result.Slug != "" {
		return result.Slug, nil
	}
	if explicit == "" && decodeErr == nil && !result.OK && result.Refusal == "no_context" {
		return "", nil
	}
	if decodeErr == nil && !result.OK {
		return "", fmt.Errorf("mission resolution refused (%s): %s; %s", fallback(result.Refusal, "unknown"), fallback(result.Reason, "mish did not resolve a mission"), fallback(result.Remedy, "correct the mission context and retry"))
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = "mish resolve did not return its JSON contract"
	}
	return "", fmt.Errorf("mission resolution failed: %s; verify mish is installed and the mission context is valid", detail)
}

func fallback(value, otherwise string) string {
	if strings.TrimSpace(value) == "" {
		return otherwise
	}
	return value
}

func commandExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func refuse(stderr io.Writer, reason string) {
	fmt.Fprintf(stderr, "herder raise: refused — %s\n", reason)
}

func printHelp(stdout io.Writer) {
	lines := []string{
		"herder raise — open a structured item at the configured human seat over hcom.",
		"",
		"Usage:",
		"  herder raise --context '<cold-open>' --expects decide|act|reply|read [--thread <slug>] [--mission <slug>] -- '<body>'",
		"",
		"Configuration:",
		"  Add {\"raise\":{\"seat\":\"<name>\"}} to herder's state config.json.",
		"  The command refuses when the seat is not configured; there is no default target.",
		"",
		"Message contract:",
		"  Line 1 is the context, line 2 is Expects: <value>, an optional line 3 is",
		"  Mission: <slug>, then a blank line and the body. --thread passes through to hcom.",
		"  decide/reply use intent=request; act/read use intent=inform.",
	}
	fmt.Fprintln(stdout, strings.Join(lines, "\n"))
}
