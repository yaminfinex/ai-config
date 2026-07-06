package lifecyclecmd

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
	"time"

	"ai-config/tools/herder/internal/herderpaths"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	"ai-config/tools/herder/internal/shellquote"
)

type forkOptions struct {
	help   bool
	json   bool
	label  string
	role   string
	prompt string
	target string
}

type resumeOptions struct {
	help   bool
	json   bool
	target string
}

func RunFork(args []string, stdout, stderr io.Writer) int {
	opts, code := parseForkArgs(args, stdout, stderr)
	if code != 0 || opts.help {
		return code
	}
	return (&runner{stdout: stdout, stderr: stderr}).fork(opts)
}

func RunResume(args []string, stdout, stderr io.Writer) int {
	opts, code := parseResumeArgs(args, stdout, stderr)
	if code != 0 || opts.help {
		return code
	}
	return (&runner{stdout: stdout, stderr: stderr}).resume(opts)
}

type runner struct {
	stdout io.Writer
	stderr io.Writer
	herdr  herdrClient
}

type herdrClient interface {
	Combined(args ...string) ([]byte, int, error)
	Output(args ...string) ([]byte, error)
}

func (r *runner) client() herdrClient {
	if r.herdr != nil {
		return r.herdr
	}
	return &herdrcli.Client{}
}

func (r *runner) fork(opts forkOptions) int {
	if code := requireTools(r.stderr); code != 0 {
		return code
	}
	recs, registryPath, code := loadRegistry(r.stderr)
	if code != 0 {
		return code
	}
	parent := registry.Resolve(recs, opts.target)
	if parent == nil {
		die(r.stderr, "unknown target: "+opts.target)
		return 1
	}
	parentGUID := ptrString(parent.GUID)
	if parentGUID == "" {
		die(r.stderr, "target has no guid: "+opts.target)
		return 1
	}
	sessionID := registry.ToolSessionIDForGUID(recs, parentGUID)
	live := liveAgents(r.client())
	liveParent := parent.Status == "active" && parent.TerminalID != "" && live[parent.TerminalID].TerminalID != nil

	vehicleTarget := ""
	if liveParent && parent.HcomName != "" {
		vehicleTarget = parent.HcomName
	} else if sessionID != "" {
		vehicleTarget = sessionID
	}
	if vehicleTarget == "" {
		die(r.stderr, fmt.Sprintf("cannot fork %s: missing live hcom_name or stored provenance.tool_session_id", opts.target))
		return 1
	}

	guid, err := registry.NewGUID()
	if err != nil {
		die(r.stderr, err.Error())
		return 1
	}
	short := registry.ShortGUID(guid)
	label := opts.label
	if label == "" {
		label = fmt.Sprintf("%s-fork-%s", firstNonEmpty(ptrString(parent.Label), "agent"), short)
	}
	if owner := registry.ActiveLabelOwner(recs, label, guid); owner != nil {
		die(r.stderr, fmt.Sprintf("label %q already belongs to active guid %s", label, ptrString(owner.GUID)))
		return 1
	}
	role := firstNonEmpty(opts.role, parent.Role, "worker")
	prov := registry.BuildProvenance("fork", role, currentCWD(), "")
	prov.ForkedFrom = parentGUID

	row, code := r.startAndAppend(startSpec{
		Mode:          "fork",
		GUID:          guid,
		Short:         short,
		Label:         label,
		Role:          role,
		Agent:         firstNonEmpty(parent.Agent, "claude"),
		HcomDir:       firstNonEmpty(parent.HcomDir, filepath.Join(os.Getenv("HOME"), ".hcom")),
		VehicleTarget: vehicleTarget,
		ParentSession: sessionID,
		Prompt:        opts.prompt,
		RegistryPath:  registryPath,
		BaseRaw:       []byte(`{}`),
		Provenance:    prov,
	})
	if code != 0 {
		return code
	}
	fmt.Fprintf(r.stderr, "forked %s -> %s (%s) pane=%s from=%s\n", firstNonEmpty(ptrString(parent.Label), opts.target), label, guid, row["pane_id"], parentGUID)
	if opts.json {
		b, _ := json.Marshal(row)
		fmt.Fprintln(r.stdout, string(b))
	}
	return 0
}

func (r *runner) resume(opts resumeOptions) int {
	if code := requireTools(r.stderr); code != 0 {
		return code
	}
	recs, registryPath, code := loadRegistry(r.stderr)
	if code != 0 {
		return code
	}
	rec := registry.Resolve(recs, opts.target)
	if rec == nil {
		die(r.stderr, "unknown target: "+opts.target)
		return 1
	}
	guid := ptrString(rec.GUID)
	if guid == "" {
		die(r.stderr, "target has no guid: "+opts.target)
		return 1
	}
	live := liveAgents(r.client())
	if rec.Status == "active" && rec.TerminalID != "" && live[rec.TerminalID].TerminalID != nil {
		die(r.stderr, fmt.Sprintf("%s is already running; use herder send/wait", firstNonEmpty(ptrString(rec.Label), opts.target)))
		return 1
	}
	sessionID := registry.ToolSessionIDForGUID(recs, guid)
	if sessionID == "" {
		die(r.stderr, fmt.Sprintf("cannot resume %s: missing provenance.tool_session_id", opts.target))
		return 1
	}
	label := firstNonEmpty(ptrString(rec.Label), "resumed-"+registry.ShortGUID(guid))
	if owner := registry.ActiveLabelOwner(recs, label, guid); owner != nil {
		die(r.stderr, fmt.Sprintf("label %q already belongs to active guid %s", label, ptrString(owner.GUID)))
		return 1
	}
	prov := registry.BuildProvenance("resume", rec.HcomTag, currentCWD(), "")
	if rec.Provenance != nil {
		prov = *rec.Provenance
	}
	prov.ToolSessionID = sessionID
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	prov.TS = now
	prov.ResumedAt = now

	base := rec.Raw
	if len(bytes.TrimSpace(base)) == 0 {
		base = []byte(`{}`)
	}
	base = registry.DropRawFields(base, "closed_at", "closed_by_pane", "close_result", "close_reason")
	row, code := r.startAndAppend(startSpec{
		Mode:          "resume",
		GUID:          guid,
		Short:         firstNonEmpty(ptrString(rec.ShortGUID), registry.ShortGUID(guid)),
		Label:         label,
		Role:          firstNonEmpty(rec.Role, rec.HcomTag, "worker"),
		Agent:         firstNonEmpty(rec.Agent, "claude"),
		HcomDir:       firstNonEmpty(rec.HcomDir, filepath.Join(os.Getenv("HOME"), ".hcom")),
		VehicleTarget: sessionID,
		RegistryPath:  registryPath,
		BaseRaw:       base,
		Provenance:    prov,
	})
	if code != 0 {
		return code
	}
	fmt.Fprintf(r.stderr, "resumed %s (%s) pane=%s\n", label, guid, row["pane_id"])
	if opts.json {
		b, _ := json.Marshal(row)
		fmt.Fprintln(r.stdout, string(b))
	}
	return 0
}

type startSpec struct {
	Mode          string
	GUID          string
	Short         string
	Label         string
	Role          string
	Agent         string
	HcomDir       string
	VehicleTarget string
	ParentSession string
	Prompt        string
	RegistryPath  string
	BaseRaw       []byte
	Provenance    registry.Provenance
}

func (r *runner) startAndAppend(spec startSpec) (map[string]any, int) {
	paths, err := herderpaths.Resolve()
	if err != nil {
		die(r.stderr, err.Error())
		return nil, 1
	}
	cwd := firstNonEmpty(os.Getenv("HERDER_LIFECYCLE_CWD"), currentCWD())
	split := firstNonEmpty(os.Getenv("HERDER_LIFECYCLE_SPLIT"), "right")
	focusFlag := firstNonEmpty(os.Getenv("HERDER_LIFECYCLE_FOCUS"), "--no-focus")
	extra := permissionArgs(spec.Agent)
	extra = append(extra, "--go")
	if spec.Prompt != "" {
		extra = append(extra, "--hcom-prompt", spec.Prompt)
	}
	launchTokens := []string{paths.BinHerder, "launch", "--" + spec.Mode, spec.Agent, spec.VehicleTarget, "--tag", spec.Role}
	if spec.Mode == "fork" && spec.ParentSession != "" {
		launchTokens = append(launchTokens, "--parent-session", spec.ParentSession)
	}
	launchTokens = append(launchTokens, extra...)

	inner := shellCommand(launchTokens)
	spawnedBy := firstNonEmpty(os.Getenv("HERDER_GUID"), "user")
	shell := firstNonEmpty(os.Getenv("SHELL"), "/bin/zsh")
	innerCmd := fmt.Sprintf("export HERDER_GUID=%s HERDER_ROLE=%s HERDER_LABEL=%s HERDER_SPAWNED_BY=%s HERDER_BIN=%s HCOM_DIR=%s; exec %s",
		shellquote.Quote(spec.GUID), shellquote.Quote(spec.Role), shellquote.Quote(spec.Label), shellquote.Quote(spawnedBy), shellquote.Quote(paths.BinHerder), shellquote.Quote(spec.HcomDir), inner)
	argv := []string{shell, "-lic", innerCmd}
	startArgs := []string{"agent", "start", spec.Label, focusFlag, "--split", split, "--cwd", cwd, "--", shell, "-lic", innerCmd}
	out, rc, _ := r.client().Combined(startArgs...)
	if rc != 0 {
		fmt.Fprintf(r.stderr, "herdr agent start failed:\n%s\n", strings.TrimRight(string(out), "\n"))
		return nil, rc
	}
	start, err := parseAgentStart(out)
	if err != nil || start.Agent.PaneID == "" {
		fmt.Fprintf(r.stderr, "unexpected start payload: %s\n", strings.TrimRight(string(out), "\n"))
		return nil, 1
	}
	spec.Provenance.CWD = firstNonEmpty(start.Agent.CWD, cwd)
	spec.Provenance.WorkspaceID = start.Agent.WorkspaceID
	row, err := registry.UpdateRawObject(spec.BaseRaw, map[string]any{
		"guid":            spec.GUID,
		"short_guid":      spec.Short,
		"label":           spec.Label,
		"role":            spec.Role,
		"agent":           spec.Agent,
		"argv":            argv,
		"pane_id":         start.Agent.PaneID,
		"terminal_id":     start.Agent.TerminalID,
		"workspace_id":    start.Agent.WorkspaceID,
		"cwd":             start.Agent.CWD,
		"started_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"started_by_pane": firstNonEmpty(os.Getenv("HERDR_PANE_ID"), "unknown"),
		"hcom_dir":        spec.HcomDir,
		"hcom_name":       "",
		"hcom_tag":        spec.Role,
		"status":          "active",
		"provenance":      spec.Provenance,
	})
	if err != nil {
		die(r.stderr, err.Error())
		return nil, 1
	}
	if err := registry.Append(spec.RegistryPath, row); err != nil {
		die(r.stderr, err.Error())
		return nil, 1
	}
	if code := r.verifyLaunchStayedAlive(spec.RegistryPath, row, start.Agent.PaneID); code != 0 {
		return nil, code
	}
	var decoded map[string]any
	_ = json.Unmarshal(row, &decoded)
	return decoded, 0
}

func (r *runner) verifyLaunchStayedAlive(registryPath string, row []byte, paneID string) int {
	settle := lifecycleSettleMS()
	if settle <= 0 {
		return 0
	}
	time.Sleep(time.Duration(settle) * time.Millisecond)
	if _, err := r.client().Output("pane", "get", paneID); err == nil {
		return 0
	}
	closed := registry.DropRawFields(row, "closed_at", "closed_by_pane", "close_result", "close_reason")
	closed, err := registry.UpdateRawObject(closed, map[string]any{
		"status":         "closed",
		"closed_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"closed_by_pane": firstNonEmpty(os.Getenv("HERDR_PANE_ID"), "unknown"),
		"close_result":   "launch_failed",
		"close_reason":   "pane exited before lifecycle bind",
	})
	if err == nil {
		_ = registry.Append(registryPath, closed)
	}
	die(r.stderr, "launch failed before lifecycle bind")
	return 1
}

func lifecycleSettleMS() int {
	value := os.Getenv("HERDER_LIFECYCLE_SETTLE_MS")
	if value == "" {
		return 7000
	}
	var n int
	_, _ = fmt.Sscanf(value, "%d", &n)
	return n
}

func shellCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellquote.Quote(arg))
	}
	return strings.Join(quoted, " ")
}

func parseForkArgs(args []string, stdout, stderr io.Writer) (forkOptions, int) {
	var opts forkOptions
	for i := 0; i < len(args); {
		switch args[i] {
		case "--label":
			if i+1 >= len(args) {
				die(stderr, "--label requires a value")
				return opts, 1
			}
			opts.label = args[i+1]
			i += 2
		case "--role":
			if i+1 >= len(args) {
				die(stderr, "--role requires a value")
				return opts, 1
			}
			opts.role = args[i+1]
			i += 2
		case "--prompt":
			if i+1 >= len(args) {
				die(stderr, "--prompt requires a value")
				return opts, 1
			}
			opts.prompt = args[i+1]
			i += 2
		case "--json":
			opts.json = true
			i++
		case "-h", "--help":
			printForkHelp(stdout)
			opts.help = true
			return opts, 0
		default:
			if strings.HasPrefix(args[i], "-") {
				die(stderr, "unknown arg: "+args[i])
				return opts, 1
			}
			if opts.target != "" {
				die(stderr, "usage: herder fork <target> [--label L] [--role R] [--prompt P] [--json]")
				return opts, 1
			}
			opts.target = args[i]
			i++
		}
	}
	if opts.target == "" {
		die(stderr, "usage: herder fork <target> [--label L] [--role R] [--prompt P] [--json]")
		return opts, 1
	}
	return opts, 0
}

func parseResumeArgs(args []string, stdout, stderr io.Writer) (resumeOptions, int) {
	var opts resumeOptions
	for i := 0; i < len(args); {
		switch args[i] {
		case "--json":
			opts.json = true
			i++
		case "-h", "--help":
			printResumeHelp(stdout)
			opts.help = true
			return opts, 0
		default:
			if strings.HasPrefix(args[i], "-") {
				die(stderr, "unknown arg: "+args[i])
				return opts, 1
			}
			if opts.target != "" {
				die(stderr, "usage: herder resume <target> [--json]")
				return opts, 1
			}
			opts.target = args[i]
			i++
		}
	}
	if opts.target == "" {
		die(stderr, "usage: herder resume <target> [--json]")
		return opts, 1
	}
	return opts, 0
}

func printForkHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder fork — branch an enrolled agent session into a new guid.

Usage:
  herder fork <target> [--label L] [--role R] [--prompt P] [--json]
`)
}

func printResumeHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder resume — reopen an enrolled agent session with the same guid.

Usage:
  herder resume <target> [--json]
`)
}

func requireTools(stderr io.Writer) int {
	if os.Getenv("HERDR_ENV") != "1" {
		die(stderr, "not running inside a herdr pane (HERDR_ENV != 1)")
		return 1
	}
	for _, tool := range []string{"herdr", "hcom"} {
		if _, err := exec.LookPath(tool); err != nil {
			die(stderr, tool+" not on PATH")
			return 1
		}
	}
	return 0
}

func loadRegistry(stderr io.Writer) ([]registry.Record, string, int) {
	path := registry.DefaultPath()
	recs, err := registry.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			die(stderr, "no registry at "+path)
			return nil, path, 1
		}
		die(stderr, err.Error())
		return nil, path, 1
	}
	return recs, path, 0
}

func liveAgents(client herdrClient) map[string]herdrcli.Agent {
	out, err := client.Output("agent", "list")
	if err != nil {
		out = []byte(`{"result":{"agents":[]}}`)
	}
	agents, err := herdrcli.ParseAgentList(out)
	if err != nil {
		agents = nil
	}
	live := make(map[string]herdrcli.Agent)
	for _, agent := range agents {
		if agent.TerminalID != nil {
			live[*agent.TerminalID] = agent
		}
	}
	return live
}

func permissionArgs(agent string) []string {
	switch agent {
	case "claude":
		return []string{"--dangerously-skip-permissions"}
	case "codex":
		return []string{"--dangerously-bypass-approvals-and-sandbox"}
	default:
		return nil
	}
}

func parseAgentStart(out []byte) (struct {
	Agent struct {
		PaneID      string `json:"pane_id"`
		TerminalID  string `json:"terminal_id"`
		WorkspaceID string `json:"workspace_id"`
		CWD         string `json:"cwd"`
	} `json:"agent"`
}, error) {
	var envelope struct {
		Result struct {
			Agent struct {
				PaneID      string `json:"pane_id"`
				TerminalID  string `json:"terminal_id"`
				WorkspaceID string `json:"workspace_id"`
				CWD         string `json:"cwd"`
			} `json:"agent"`
		} `json:"result"`
	}
	err := json.Unmarshal(out, &envelope)
	return envelope.Result, err
}

func currentCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder-lifecycle: %s\n", msg)
}
