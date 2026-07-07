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
	self   bool
	label  string
	role   string
	prompt string
	split  string
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
	r := &runner{stdout: stdout, stderr: stderr}
	if opts.self {
		return r.forkSelf(opts)
	}
	return r.fork(opts)
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
	if opts.split != "" {
		os.Setenv("HERDER_LIFECYCLE_SPLIT", opts.split)
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
		die(r.stderr, fmt.Sprintf("cannot fork %s: no live parent and no recorded tool_session_id — nothing to fork from; spawn a fresh agent instead", opts.target))
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
	// spawned_by is the session that RAN this fork ($HERDER_GUID, matching the
	// HERDER_SPAWNED_BY that startAndAppend exports into the child's pane); the
	// forker's own spawner stays reachable transitively via the forker's row.
	prov := registry.BuildProvenance("fork", firstNonEmpty(os.Getenv("HERDER_GUID"), "user"), role, currentCWD(), "")
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

// forkSelf forks the CURRENT session — "fork me, right now, from this pane" —
// auto-detecting the tool and identity from the environment. A registered pane
// routes through the native fork path (bus-bound child, provenance.forked_from);
// an unregistered one falls back to a raw tool fork/resume through spawn.
func (r *runner) forkSelf(opts forkOptions) int {
	if code := requireTools(r.stderr); code != 0 {
		return code
	}
	paneEnvID := os.Getenv("HERDR_PANE_ID")
	if paneEnvID == "" {
		die(r.stderr, "HERDR_PANE_ID not set; cannot anchor a self-fork to the current pane — run 'herder fork --self' from inside a herdr-managed agent pane")
		return 1
	}
	agent, ok := detectSelfAgent()
	if !ok {
		die(r.stderr, "could not detect the current tool from the environment (not claude, not codex) — run 'herder fork --self' from inside a claude or codex pane, or fork a known target with 'herder fork <guid>'")
		return 1
	}
	recs, _, code := loadRegistry(r.stderr)
	if code != 0 {
		return code
	}

	// Resolve the pane's cwd. Sessions key on the project dir, so the fork must
	// land where the pane is actually working: pane foreground_cwd/cwd first, then
	// its workspace checkout, then this process's cwd as a last resort.
	paneOut, _, _ := r.client().Combined("pane", "get", paneEnvID)
	pane, _ := herdrcli.ParsePaneGet(paneOut)
	cwd := r.resolveSelfCWD(pane)

	// Correlate the pane to a registered guid. HERDER_GUID (exported into every
	// herder-spawned pane) is the direct key; otherwise map pane -> hcom identity
	// -> registry row via hcom_name or the recorded tool_session_id.
	name, hcomSession := "", ""
	if pane.PaneID != "" {
		name, hcomSession = currentHcomIdentity(pane.PaneID)
	}
	sessionID := firstNonEmpty(os.Getenv("CLAUDE_CODE_SESSION_ID"), hcomSession)
	nativeGUID := os.Getenv("HERDER_GUID")
	if nativeGUID == "" {
		nativeGUID = selfMatchGUID(recs, name, sessionID)
	}

	// A registered claude session forks natively so the child is bus-bound from
	// birth and carries provenance.forked_from. Codex and unregistered claude
	// sessions have no native fork; they fall back to a raw tool fork through spawn.
	if agent == "claude" && nativeGUID != "" {
		os.Setenv("HERDER_LIFECYCLE_CWD", cwd)
		os.Setenv("HERDER_LIFECYCLE_FOCUS", "--no-focus")
		return r.fork(forkOptions{
			target: nativeGUID,
			label:  opts.label,
			role:   opts.role,
			prompt: opts.prompt,
			split:  firstNonEmpty(opts.split, "right"),
			json:   opts.json,
		})
	}
	return r.forkSelfFallback(opts, agent, paneEnvID, cwd, sessionID)
}

// forkSelfFallback hands off to `herder spawn`, which re-forks the tool in a
// fresh pane: claude via `--resume <session> --fork-session`, codex via
// `fork <session>` (or `fork --last`).
func (r *runner) forkSelfFallback(opts forkOptions, agent, paneEnvID, cwd, sessionID string) int {
	split := firstNonEmpty(opts.split, "right")
	role := firstNonEmpty(opts.role, "fork-"+agent)
	var agentArgs []string
	switch agent {
	case "claude":
		if sessionID == "" {
			die(r.stderr, "no registered herder guid and no claude session id to fork from — run 'herder enroll' to register this session, set CLAUDE_CODE_SESSION_ID, or fork a known target with 'herder fork <guid>'")
			return 1
		}
		agentArgs = []string{"--resume", sessionID, "--fork-session"}
	case "codex":
		if sessionID != "" {
			agentArgs = []string{"fork", sessionID}
		} else {
			agentArgs = []string{"fork", "--last"}
		}
	default:
		die(r.stderr, "unknown tool: "+agent)
		return 1
	}

	paths, err := herderpaths.Resolve()
	if err != nil {
		die(r.stderr, err.Error())
		return 1
	}
	herderBin := firstNonEmpty(os.Getenv("HERDER_BIN"), paths.BinHerder)

	spawnArgs := []string{"spawn", "--role", role, "--agent", agent, "--from-pane", paneEnvID, "--cwd", cwd, "--split", split, "--no-focus"}
	for _, a := range agentArgs {
		spawnArgs = append(spawnArgs, "--extra-arg", a)
	}
	if opts.prompt != "" {
		spawnArgs = append(spawnArgs, "--prompt", opts.prompt)
	}

	cmd := exec.Command(herderBin, spawnArgs...)
	cmd.Stdout = r.stdout
	cmd.Stderr = r.stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		die(r.stderr, "failed to hand off to herder spawn: "+err.Error())
		return 1
	}
	return 0
}

// detectSelfAgent identifies the tool running the current session from its env,
// checking claude markers before codex.
func detectSelfAgent() (string, bool) {
	if os.Getenv("CLAUDECODE") == "1" || os.Getenv("CLAUDE_CODE_SESSION_ID") != "" {
		return "claude", true
	}
	if strings.HasPrefix(os.Getenv("AI_AGENT"), "claude-code") {
		return "claude", true
	}
	if os.Getenv("CODEX_HOME") != "" {
		return "codex", true
	}
	return "", false
}

func (r *runner) resolveSelfCWD(pane herdrcli.Pane) string {
	if pane.ForegroundCWD != "" {
		return pane.ForegroundCWD
	}
	if pane.CWD != "" {
		return pane.CWD
	}
	if pane.WorkspaceID != "" {
		if cwd := r.workspaceCheckout(pane.WorkspaceID); cwd != "" {
			return cwd
		}
	}
	return currentCWD()
}

func (r *runner) workspaceCheckout(wsID string) string {
	out, err := r.client().Output("workspace", "list")
	if err != nil {
		return ""
	}
	var envelope struct {
		Result struct {
			Workspaces []struct {
				WorkspaceID string `json:"workspace_id"`
				Worktree    struct {
					CheckoutPath string `json:"checkout_path"`
					RepoRoot     string `json:"repo_root"`
				} `json:"worktree"`
				CWD string `json:"cwd"`
			} `json:"workspaces"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &envelope) != nil {
		return ""
	}
	for _, ws := range envelope.Result.Workspaces {
		if ws.WorkspaceID == wsID {
			return firstNonEmpty(ws.Worktree.CheckoutPath, ws.Worktree.RepoRoot, ws.CWD)
		}
	}
	return ""
}

// currentHcomIdentity maps a pane to its hcom (name, session_id) by scanning
// `hcom list --json` for the entry launched in that pane. The last match wins,
// mirroring the script's `tail -n1`.
func currentHcomIdentity(paneID string) (name, session string) {
	out, err := exec.Command("hcom", "list", "--json").Output()
	if err != nil {
		return "", ""
	}
	var entries []struct {
		Name          string `json:"name"`
		SessionID     string `json:"session_id"`
		LaunchContext struct {
			PaneID string `json:"pane_id"`
		} `json:"launch_context"`
	}
	if json.Unmarshal(out, &entries) != nil {
		return "", ""
	}
	for _, e := range entries {
		if e.LaunchContext.PaneID == paneID {
			name, session = e.Name, e.SessionID
		}
	}
	return name, session
}

// selfMatchGUID finds the registered guid for the current session, matching a
// row by hcom_name or recorded tool_session_id. Latest-per-guid, greatest-guid
// tie-break — the script's group_by/last.
func selfMatchGUID(recs []registry.Record, name, session string) string {
	guid := ""
	for _, rec := range registry.LatestByGUID(recs) {
		match := (name != "" && rec.HcomName == name) ||
			(session != "" && rec.Provenance != nil && rec.Provenance.ToolSessionID == session)
		if match && rec.GUID != nil {
			guid = *rec.GUID
		}
	}
	return guid
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
		die(r.stderr, fmt.Sprintf("cannot resume %s: no tool_session_id recorded for this guid (never captured, or predates session capture) — spawn a fresh agent instead", opts.target))
		return 1
	}
	label := firstNonEmpty(ptrString(rec.Label), "resumed-"+registry.ShortGUID(guid))
	if owner := registry.ActiveLabelOwner(recs, label, guid); owner != nil {
		die(r.stderr, fmt.Sprintf("label %q already belongs to active guid %s", label, ptrString(owner.GUID)))
		return 1
	}
	// No-prior-provenance fallback: spawned_by is the session performing this
	// resume ($HERDER_GUID), not the ambient grandparent. Normally overwritten
	// by the preserved prior provenance just below.
	prov := registry.BuildProvenance("resume", firstNonEmpty(os.Getenv("HERDER_GUID"), "user"), rec.HcomTag, currentCWD(), "")
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
		case "--split":
			if i+1 >= len(args) {
				die(stderr, "--split requires a value")
				return opts, 1
			}
			opts.split = args[i+1]
			i += 2
		case "--self":
			opts.self = true
			i++
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
				die(stderr, "usage: herder fork <target> [--label L] [--role R] [--prompt P] [--json] | herder fork --self [--split D] ...")
				return opts, 1
			}
			opts.target = args[i]
			i++
		}
	}
	if opts.self && opts.target != "" {
		die(stderr, "cannot combine --self with a positional target; fork THIS session (--self) or a named one (<target>), not both")
		return opts, 1
	}
	if !opts.self && opts.target == "" {
		die(stderr, "usage: herder fork <target> [--label L] [--role R] [--prompt P] [--json] | herder fork --self [--split D] ...")
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
	fmt.Fprint(stdout, `herder fork — branch an agent's session into a NEW guid, in a new pane.

Forks a conversation into a fresh agent with its own guid, so the child starts
from the parent's context but diverges independently. Name a target to fork an
enrolled peer; pass --self to fork THIS session ("fork me, from this pane"),
auto-detecting the current tool and identity from the environment.

Usage:
  herder fork <target> [--label L] [--role R] [--prompt P] [--split D] [--json]
  herder fork --self    [--label L] [--role R] [--prompt P] [--split D] [--json]

Options:
  --self       fork the current session instead of a named target (mutually
               exclusive with <target>)
  --label L    label for the fork (default: <parent>-fork-<short>)
  --role R     role / hcom tag for the fork (default: parent's role, else worker;
               --self fallback default: fork-<tool>)
  --prompt P   initial prompt delivered to the fork once it is ready
  --split D    pane split for the new pane: right (default) or down
  --json       print the new registry record as JSON on stdout

Behavior:
  A named target (or a --self pane that resolves to a registered claude guid)
  forks NATIVELY: the child is bus-bound from birth and records
  provenance.forked_from. Needs a live parent (forks off its bus name) or a
  recorded tool_session_id.

  Child provenance: forked_from is the content parent (the forked target);
  spawned_by is the session that RAN the fork (its HERDER_GUID, else "user") —
  NOT that session's own spawner, which stays reachable transitively via the
  forker's row.

  --self with no registered guid (codex, or an unenrolled claude) FALLS BACK to a
  raw tool fork through 'herder spawn': claude via '--resume <session>
  --fork-session', codex via 'fork <session>' (else 'fork --last'). Tool and cwd
  are detected from the pane; cwd tracks the pane's foreground dir so the session
  key resolves.

Exit codes:
  0  fork launched (native) or handed off to spawn (fallback)
  1  refusal or launch failure — see the message

If it fails:
  - "could not detect the current tool": --self was run outside a claude/codex
    pane — run it from inside one, or fork a known target with 'herder fork <guid>'.
  - "HERDR_PANE_ID not set": --self needs a herdr-managed pane to anchor to.
  - "no registered herder guid and no claude session id": nothing to fork from —
    'herder enroll' this session first, or set CLAUDE_CODE_SESSION_ID.
  - "cannot fork ...: no live parent and no recorded tool_session_id": the parent
    is not live and no session was ever captured — spawn a fresh agent instead.
  - "unknown target": run 'herder list --all' to find the right guid/label.
`)
}

func printResumeHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder resume — reopen an enrolled agent's session under the SAME guid, in a new pane.

Resumes a closed or dead agent from its recorded tool_session_id, keeping the same
guid and label so its registry identity stays continuous. Only works if a session
id was captured for that guid.

Usage:
  herder resume <target> [--json]

Options:
  --json    print the new registry record as JSON on stdout

If it fails:
  - "already running": the agent is live — use herder send/wait, not resume.
  - "cannot resume ...: no tool_session_id recorded for this guid": its session was
    never captured (or it predates session capture) — spawn a fresh agent instead.
  - "unknown target": run 'herder list --all' to find the right guid/label.
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
