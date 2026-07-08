// Package herdrcli execs the herdr CLI and decodes the response payloads the
// bash substrate consumes. It is deliberately thin: the scripts' behavior on
// herdr failure varies per call site (`|| true`, `|| printf '{"result":…}'`,
// exit-code propagation for `herdr wait`), so the client exposes the raw
// output and exit code and lets each ported command reproduce its own
// fallback exactly. Payload decoding mirrors the scripts' jq paths
// (`.result.agents[]?`, `.result.pane.terminal_id // empty`, …): missing or
// null result members decode to zero values, invalid JSON is an error.
//
// The hermetic suites exercise this seam with a mock `herdr` on PATH, which
// is why lookup goes through the environment like the bash scripts' bare
// `herdr` invocation.
package herdrcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
)

// Client runs herdr commands. The zero value resolves `herdr` on PATH, the
// same way the bash scripts invoke it (and how the suites' mocks intercept).
type Client struct {
	// Bin overrides the executable; empty means "herdr" via PATH lookup.
	Bin string
}

func (c *Client) bin() string {
	if c != nil && c.Bin != "" {
		return c.Bin
	}
	return "herdr"
}

// Available reports whether the herdr executable can be resolved — the
// scripts' `command -v herdr` guard.
func (c *Client) Available() bool {
	_, err := exec.LookPath(c.bin())
	return err == nil
}

// Output runs herdr and returns its stdout, discarding stderr (the scripts'
// `herdr … 2>/dev/null` shape). A non-zero exit is returned as an error with
// whatever stdout was produced.
func (c *Client) Output(args ...string) ([]byte, error) {
	cmd := exec.Command(c.bin(), args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	return stdout.Bytes(), err
}

// Combined runs herdr with stdout and stderr interleaved into one buffer
// (the scripts' `herdr … 2>&1` shape) and returns the exit code alongside.
// err is non-nil only when the command could not run at all.
func (c *Client) Combined(args ...string) (out []byte, exitCode int, err error) {
	cmd := exec.Command(c.bin(), args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err = cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return buf.Bytes(), exitErr.ExitCode(), nil
	}
	if err != nil {
		return buf.Bytes(), -1, err
	}
	return buf.Bytes(), 0, nil
}

// Run executes herdr for its exit code only, discarding all output (the
// `herdr wait … >/dev/null 2>&1` shape). err is non-nil only when the
// command could not run at all.
func (c *Client) Run(args ...string) (exitCode int, err error) {
	cmd := exec.Command(c.bin(), args...)
	err = cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

// Agent is one entry of `herdr agent list` (.result.agents[]). TerminalID
// stays a pointer because herder list's reconcile keys its live map on
// `select(.terminal_id != null)` — null and "" must stay distinguishable.
// Raw preserves the full object: reconcile embeds it verbatim as the
// record's `live` field, including members this struct doesn't name.
type Agent struct {
	TerminalID *string `json:"terminal_id"`
	PaneID     string  `json:"pane_id"`
	Agent      string  `json:"agent"`
	Status     string  `json:"agent_status"`
	Name       string  `json:"name"`
	CWD        string  `json:"cwd"`

	Raw json.RawMessage `json:"-"`
}

// Pane is one pane object, from `herdr pane list` (.result.panes[]) or
// `herdr pane get` (.result.pane).
type Pane struct {
	PaneID        string `json:"pane_id"`
	TerminalID    string `json:"terminal_id"`
	WorkspaceID   string `json:"workspace_id"`
	CWD           string `json:"cwd"`
	ForegroundCWD string `json:"foreground_cwd"`
}

// Workspace is one entry of `herdr workspace list` (.result.workspaces[]).
type Workspace struct {
	WorkspaceID string `json:"workspace_id"`
}

// Tab is `herdr tab create`'s .result.tab.
type Tab struct {
	TabID string `json:"tab_id"`
}

// AgentStart is `herdr agent start`'s result:
// {"id":…,"result":{"agent":{…},"argv":[…],"type":"agent_started"}}.
type AgentStart struct {
	Agent struct {
		PaneID      string `json:"pane_id"`
		WorkspaceID string `json:"workspace_id"`
		TabID       string `json:"tab_id"`
		TerminalID  string `json:"terminal_id"`
	} `json:"agent"`
	Argv []string `json:"argv"`
	Type string   `json:"type"`
}

// ParseAgentList decodes `herdr agent list` output. Like `.result.agents[]?`
// a missing/null agents array yields no entries without erroring.
func ParseAgentList(out []byte) ([]Agent, error) {
	var envelope struct {
		Result struct {
			Agents []json.RawMessage `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, err
	}
	var agents []Agent
	for _, raw := range envelope.Result.Agents {
		var a Agent
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		a.Raw = raw
		agents = append(agents, a)
	}
	return agents, nil
}

// ParsePaneList decodes `herdr pane list` output (.result.panes[]?).
func ParsePaneList(out []byte) ([]Pane, error) {
	var envelope struct {
		Result struct {
			Panes []Pane `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, err
	}
	return envelope.Result.Panes, nil
}

// ParsePaneGet decodes `herdr pane get` output (.result.pane). A missing
// pane member decodes to the zero Pane, matching `// empty` fallbacks.
func ParsePaneGet(out []byte) (Pane, error) {
	var envelope struct {
		Result struct {
			Pane Pane `json:"pane"`
		} `json:"result"`
	}
	err := json.Unmarshal(out, &envelope)
	return envelope.Result.Pane, err
}

// ParseWorkspaceList decodes `herdr workspace list` output
// (.result.workspaces[]?).
func ParseWorkspaceList(out []byte) ([]Workspace, error) {
	var envelope struct {
		Result struct {
			Workspaces []Workspace `json:"workspaces"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, err
	}
	return envelope.Result.Workspaces, nil
}

// ParseTabCreate decodes `herdr tab create` output (.result.tab.tab_id).
func ParseTabCreate(out []byte) (Tab, error) {
	var envelope struct {
		Result struct {
			Tab Tab `json:"tab"`
		} `json:"result"`
	}
	err := json.Unmarshal(out, &envelope)
	return envelope.Result.Tab, err
}

// ParseAgentStart decodes `herdr agent start` output.
func ParseAgentStart(out []byte) (AgentStart, error) {
	var envelope struct {
		Result AgentStart `json:"result"`
	}
	err := json.Unmarshal(out, &envelope)
	return envelope.Result, err
}
