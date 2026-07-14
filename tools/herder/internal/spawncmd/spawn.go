package spawncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ai-config/tools/herder/internal/grokbridge"
	"ai-config/tools/herder/internal/herderpaths"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/launchcmd"
	"ai-config/tools/herder/internal/observercmd"
	"ai-config/tools/herder/internal/panecleanup"
	"ai-config/tools/herder/internal/placement"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/send"
	"ai-config/tools/herder/internal/shellquote"
)

const trustModalPattern = `Do you trust the contents of this directory|Do you trust the files in this folder|Is this a project you created or one you trust|Yes, I trust this folder`

var trustModalRE = regexp.MustCompile(trustModalPattern)

// misePathFix re-pins mise's shims dir to the FRONT of the child's PATH inside
// the login-shell wrapper. Rationale: `mise activate` in rc files is prompt-
// hook driven, and a spawned pane inherits stale __MISE_* session state, so in
// `-lic` mode the rc chain can leave system dirs (/usr/bin) ahead of mise's
// tool paths — the child then resolves e.g. the OS go over the pinned one.
// Shims are position-proof (each shim re-resolves the tool for the child's cwd
// at call time), so one prepend restores mise-activated ordering without
// parsing or re-running the activation. No mise on the machine → no dir → no-op.
const misePathFix = `if [ -d "${MISE_DATA_DIR:-$HOME/.local/share/mise}/shims" ]; then export PATH="${MISE_DATA_DIR:-$HOME/.local/share/mise}/shims:$PATH"; fi; `

type options struct {
	Help          bool
	Role          string
	Agent         string
	Prompt        string
	PromptFile    string
	Split         string
	SplitExplicit bool
	Workspace     string
	FromPane      string
	Tab           string
	NewTab        bool
	Worktree      string
	Base          string
	CWD           string
	FocusFlag     string
	LabelPrefix   string
	ExtraArgs     []string
	Model         string
	JSONOutput    bool
	WaitTimeoutMS int
	BindTimeoutMS int
	VerifyMS      int
	ReadyMatch    string
	NoReadyWait   bool
	LoginShell    bool
	LoginShellBin string
	Safe          bool
	Team          string
	Notify        bool
	NotifyTo      string
	NotifyBusName string
	SettleMS      int
}

type spawnRecord struct {
	GUID                 string              `json:"guid"`
	ShortGUID            string              `json:"short_guid"`
	Label                string              `json:"label"`
	Role                 string              `json:"role"`
	Agent                string              `json:"agent"`
	ExtraArgs            []string            `json:"extra_args"`
	Argv                 []string            `json:"argv"`
	PaneID               string              `json:"pane_id"`
	WorkspaceID          string              `json:"workspace_id"`
	TabID                string              `json:"tab_id"`
	TerminalID           string              `json:"terminal_id"`
	CWD                  string              `json:"cwd"`
	StartedAt            string              `json:"started_at"`
	StartedByPane        string              `json:"started_by_pane"`
	InitialPromptPresent bool                `json:"initial_prompt_present"`
	Team                 string              `json:"team"`
	HcomDir              string              `json:"hcom_dir"`
	HcomName             string              `json:"hcom_name"`
	HcomTag              string              `json:"hcom_tag"`
	Status               string              `json:"status"`
	Provenance           registry.Provenance `json:"provenance"`
}

type spawnJSONRecord struct {
	GUID                 string              `json:"guid"`
	ShortGUID            string              `json:"short_guid"`
	Label                string              `json:"label"`
	Role                 string              `json:"role"`
	Agent                string              `json:"agent"`
	ExtraArgs            []string            `json:"extra_args"`
	Argv                 []string            `json:"argv"`
	PaneID               string              `json:"pane_id"`
	WorkspaceID          string              `json:"workspace_id"`
	TabID                string              `json:"tab_id"`
	TerminalID           string              `json:"terminal_id"`
	CWD                  string              `json:"cwd"`
	StartedAt            string              `json:"started_at"`
	StartedByPane        string              `json:"started_by_pane"`
	InitialPromptPresent bool                `json:"initial_prompt_present"`
	Team                 string              `json:"team"`
	HcomDir              string              `json:"hcom_dir"`
	HcomName             string              `json:"hcom_name"`
	HcomTag              string              `json:"hcom_tag"`
	Status               string              `json:"status"`
	Provenance           registry.Provenance `json:"provenance"`
	PromptSent           bool                `json:"prompt_sent"`
	DeliveryResult       string              `json:"delivery_result"`
	ResendCommand        string              `json:"resend_command,omitempty"`
	PasteNotes           []string            `json:"paste_notes,omitempty"`
	PermInjected         string              `json:"perm_injected"`
	NewTab               bool                `json:"new_tab"`
	RootPaneClosed       bool                `json:"root_pane_closed"`
	NewTabResult         string              `json:"new_tab_result,omitempty"`
	HcomCapture          string              `json:"hcom_capture"`
	Worktree             *worktreeInfo       `json:"worktree,omitempty"`
}

// worktreeInfo is the --worktree coordinate block surfaced in the summary and
// the --json record, so an orchestrator can manage the workspace lifecycle
// (reuse, remove) without re-querying herdr.
type worktreeInfo struct {
	Branch       string `json:"branch"`
	Base         string `json:"base,omitempty"`
	CheckoutPath string `json:"checkout_path"`
	WorkspaceID  string `json:"workspace_id"`
}

type workspace struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Worktree    struct {
		CheckoutPath string `json:"checkout_path"`
		RepoRoot     string `json:"repo_root"`
	} `json:"worktree"`
}

type hcomEntry struct {
	Name          string        `json:"name"`
	Tag           string        `json:"tag"`
	Directory     string        `json:"directory"`
	CreatedAt     hcomCreatedAt `json:"created_at"`
	LaunchContext struct {
		PaneID string `json:"pane_id"`
	} `json:"launch_context"`
}

type hcomCreatedAt string

func (t *hcomCreatedAt) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = hcomCreatedAt(s)
		return nil
	}
	*t = hcomCreatedAt(string(b))
	return nil
}

func Run(args []string, stdout, stderr io.Writer) int {
	if os.Getenv("HERDR_ENV") != "1" {
		die(stderr, "not running inside a herdr pane (HERDR_ENV != 1)")
		return 1
	}
	if _, err := exec.LookPath("herdr"); err != nil {
		die(stderr, "herdr not on PATH")
		return 1
	}
	if _, err := exec.LookPath("jq"); err != nil {
		die(stderr, "jq not on PATH")
		return 1
	}

	opts, code := parseArgs(args, stdout, stderr)
	if code != 0 {
		return code
	}
	if opts.Help {
		return 0
	}

	runner := &runner{
		opts:   opts,
		stdout: stdout,
		stderr: stderr,
		herdr:  &herdrcli.Client{},
	}
	return runner.run()
}

type runner struct {
	opts           options
	stdout         io.Writer
	stderr         io.Writer
	herdr          herdrClient
	paths          herderpaths.Paths
	updateRegistry func(string, registry.LockedUpdateFunc) ([]registry.WriteOutcome, error)
}

type herdrClient interface {
	Combined(args ...string) ([]byte, int, error)
	Output(args ...string) ([]byte, error)
	Run(args ...string) (int, error)
}

func (r *runner) updateLocked(path string, fn registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
	if r.updateRegistry != nil {
		return r.updateRegistry(path, fn)
	}
	return registry.UpdateLocked(path, fn)
}

func (r *runner) failAfterLaunch(reason, paneID, terminalID string) int {
	cleanup := panecleanup.CloseConfirmed(r.herdr, paneID, terminalID)
	if cleanup.Confirmed {
		die(r.stderr, reason+"; launched pane cleanup confirmed: "+cleanup.Detail)
	} else {
		die(r.stderr, reason+"; launched pane cleanup FAILED: "+cleanup.Detail+" (pane may still be running)")
	}
	return 1
}

func (r *runner) registerSpawn(registryPath string, record spawnRecord) error {
	regRec := registry.Record{
		GUID:       &record.GUID,
		ShortGUID:  &record.ShortGUID,
		Label:      &record.Label,
		Role:       record.Role,
		Agent:      record.Agent,
		PaneID:     record.PaneID,
		TerminalID: record.TerminalID,
		HcomDir:    record.HcomDir,
		HcomName:   record.HcomName,
		HcomTag:    record.HcomTag,
		Status:     record.Status,
		Provenance: &record.Provenance,
	}
	outcomes, err := r.updateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		if owner := registry.V2LabelOwner(tx.Projection, record.Label, record.GUID); owner != nil {
			return nil, fmt.Errorf("label %q already belongs to non-retired session %s", record.Label, owner.GUID)
		}
		row := registry.V2FromRecord(regRec, "registered", v2.StateSeated, record.StartedAt)
		row.Provenance.CWD = record.CWD
		row.Provenance.WorkspaceID = record.WorkspaceID
		return []v2.SessionRecord{row}, nil
	})
	if err != nil {
		return err
	}
	outcome, err := registry.SingleOutcome(outcomes)
	if err != nil {
		return err
	}
	return outcome.Err()
}

func (r *runner) registerSpawnOrRollback(registryPath string, record spawnRecord) int {
	if err := r.registerSpawn(registryPath, record); err != nil {
		return r.failAfterLaunch("registry write refused: "+err.Error(), record.PaneID, record.TerminalID)
	}
	return 0
}

func newTabMoveArgs(paneID, label, focusFlag string) []string {
	return []string{"pane", "move", paneID, "--new-tab", firstNonEmpty(focusFlag, "--no-focus"), "--label", label}
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	opts := options{
		Split:         "right",
		FocusFlag:     "--no-focus",
		WaitTimeoutMS: envInt("HERDER_SPAWN_WAIT_MS", 15000),
		BindTimeoutMS: envInt("HERDER_SPAWN_BIND_MS", 60000),
		VerifyMS:      envInt("HERDER_SPAWN_VERIFY_MS", 20000),
		LoginShell:    true,
		LoginShellBin: firstNonEmpty(os.Getenv("HERDER_SPAWN_SHELL"), os.Getenv("SHELL"), "/bin/bash"),
		SettleMS:      envInt("HERDER_SPAWN_SETTLE_MS", 1500),
		Team:          os.Getenv("HERDER_TEAM"),
	}
	for i := 0; i < len(args); {
		arg := args[i]
		value := func() (string, bool) {
			if i+1 >= len(args) {
				die(stderr, "unknown arg: "+arg)
				return "", false
			}
			return args[i+1], true
		}
		switch arg {
		case "--role":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.Role = v
			i += 2
		case "--agent":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.Agent = v
			i += 2
		case "--prompt":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.Prompt = v
			i += 2
		case "--prompt-file":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.PromptFile = v
			i += 2
		case "--split":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.Split = v
			opts.SplitExplicit = true
			i += 2
		case "--workspace":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.Workspace = v
			i += 2
		case "--from-pane":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.FromPane = v
			i += 2
		case "--tab":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.Tab = v
			i += 2
		case "--new-tab":
			opts.NewTab = true
			i++
		case "--worktree":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.Worktree = v
			i += 2
		case "--base":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.Base = v
			i += 2
		case "--cwd":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.CWD = v
			i += 2
		case "--focus":
			opts.FocusFlag = "--focus"
			i++
		case "--no-focus":
			opts.FocusFlag = "--no-focus"
			i++
		case "--label-prefix":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.LabelPrefix = v
			i += 2
		case "--extra-arg":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.ExtraArgs = append(opts.ExtraArgs, v)
			i += 2
		case "--model":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			if strings.TrimSpace(v) == "" {
				die(stderr, "--model requires a non-empty model id")
				return opts, 1
			}
			opts.Model = v
			i += 2
		case "--json":
			opts.JSONOutput = true
			i++
		case "--wait-timeout-ms":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				die(stderr, "--wait-timeout-ms must be numeric: "+v)
				return opts, 1
			}
			opts.WaitTimeoutMS = n
			i += 2
		case "--ready-match":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.ReadyMatch = v
			i += 2
		case "--no-ready-wait":
			opts.NoReadyWait = true
			i++
		case "--no-login-shell":
			opts.LoginShell = false
			i++
		case "--login-shell":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.LoginShellBin = v
			i += 2
		case "--safe":
			opts.Safe = true
			i++
		case "--team":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.Team = v
			i += 2
		case "--notify":
			opts.Notify = true
			i++
		case "--notify-to":
			v, ok := value()
			if !ok {
				return opts, 1
			}
			opts.Notify = true
			opts.NotifyTo = v
			i += 2
		case "-h", "--help":
			printHelp(stdout)
			opts.Help = true
			return opts, 0
		default:
			die(stderr, "unknown arg: "+arg)
			return opts, 1
		}
	}

	if opts.Role == "" {
		die(stderr, "--role required")
		return opts, 1
	}
	if opts.Agent == "" {
		die(stderr, "--agent required")
		return opts, 1
	}
	if opts.Agent == "grok" {
		if err := launchcmd.ValidateGrokExtraArgs(opts.ExtraArgs, opts.Model != ""); err != nil {
			die(stderr, err.Error())
			return opts, 1
		}
	}
	if opts.Model != "" && opts.Agent != "claude" && opts.Agent != "codex" && opts.Agent != "grok" {
		die(stderr, "--model is supported only for --agent claude, codex, or grok; use --extra-arg for another agent's model option")
		return opts, 1
	}
	if opts.Model != "" && hasModelExtraArg(opts.Agent, opts.ExtraArgs) {
		die(stderr, "--model conflicts with a model pin in --extra-arg; use the first-class --model flag or the passthrough form, not both")
		return opts, 1
	}
	if opts.Model != "" {
		opts.ExtraArgs = append([]string{"--model", opts.Model}, opts.ExtraArgs...)
	}
	if opts.Team != "" && !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(opts.Team) {
		die(stderr, "--team must be a single safe path segment (letters/digits/._- only): "+opts.Team)
		return opts, 1
	}
	if launchcmd.IsHcomCapable(opts.Agent) && !regexp.MustCompile(`^[A-Za-z0-9-]+$`).MatchString(opts.Role) {
		die(stderr, "--role must contain only letters, digits, and hyphens (it becomes the hcom --tag): "+opts.Role)
		return opts, 1
	}
	if opts.Prompt != "" && opts.PromptFile != "" {
		die(stderr, "use --prompt or --prompt-file, not both")
		return opts, 1
	}
	if opts.Notify && opts.Prompt == "" && opts.PromptFile == "" {
		die(stderr, "--notify requires --prompt/--prompt-file (the notify appendix rides the initial prompt)")
		return opts, 1
	}
	if opts.Workspace != "" && opts.FromPane != "" {
		die(stderr, "use --workspace or --from-pane, not both")
		return opts, 1
	}
	if opts.Base != "" && opts.Worktree == "" {
		die(stderr, "--base requires --worktree")
		return opts, 1
	}
	if opts.Worktree != "" {
		if opts.Workspace != "" || opts.FromPane != "" {
			die(stderr, "use --worktree or --workspace/--from-pane, not both (--worktree creates its own workspace)")
			return opts, 1
		}
		if opts.CWD != "" {
			die(stderr, "use --worktree or --cwd, not both (the worktree's checkout is the cwd)")
			return opts, 1
		}
		if opts.Tab != "" || opts.NewTab {
			die(stderr, "use --worktree or --tab/--new-tab, not both (--worktree already gives the agent its own fresh workspace and tab)")
			return opts, 1
		}
	}
	decision, err := placement.Resolve(placement.Flags{
		Split:         opts.Split,
		SplitExplicit: opts.SplitExplicit,
		NewTab:        opts.NewTab,
		ExistingTab:   opts.Tab,
		Worktree:      opts.Worktree != "",
	})
	if err != nil {
		die(stderr, err.Error())
		return opts, 1
	}
	opts.Split = decision.Split
	opts.NewTab = decision.NewTab
	return opts, 0
}

func (r *runner) run() int {
	var err error
	r.paths, err = herderpaths.Resolve()
	if err != nil {
		die(r.stderr, err.Error())
		return 1
	}

	opts := &r.opts
	var wsListOut []byte
	var workspaces []workspace

	// Default the target to the CALLER's own pane when neither --workspace nor
	// --from-pane was given. Without an anchor the new tab/pane lands in whatever
	// workspace currently has FOCUS (wherever the human is looking), not where the
	// spawn was aimed; anchoring to HERDR_PANE_ID makes placement deterministic.
	// --worktree needs no anchor: it creates its own workspace below.
	if opts.Workspace == "" && opts.FromPane == "" && opts.Worktree == "" {
		if paneID := os.Getenv("HERDR_PANE_ID"); paneID != "" {
			opts.FromPane = paneID
		}
	}

	if opts.FromPane != "" {
		out, _, _ := r.herdr.Combined("pane", "get", opts.FromPane)
		pane, parseErr := herdrcli.ParsePaneGet(out)
		if parseErr == nil {
			opts.Workspace = pane.WorkspaceID
		}
		if opts.Workspace == "" {
			fmt.Fprintf(r.stderr, "herder spawn: --from-pane %s: pane not found (herdr pane get returned: %s)\n", opts.FromPane, strings.TrimRight(string(out), "\n"))
			return 1
		}
	}

	if opts.Workspace != "" {
		wsListOut, _, _ = r.herdr.Combined("workspace", "list")
		workspaces = parseWorkspaces(wsListOut)
		if !workspaceExists(workspaces, opts.Workspace) {
			fmt.Fprintf(r.stderr, "herder spawn: --workspace %s not found in live workspace list.\n", opts.Workspace)
			fmt.Fprintln(r.stderr, "Herdr workspace ids are session-live; they change after a herdr restart.")
			fmt.Fprintln(r.stderr, "Live workspaces:")
			for _, ws := range workspaces {
				path := firstNonEmpty(ws.Worktree.CheckoutPath, ws.Worktree.RepoRoot)
				label := ws.Label
				if label == "" {
					label = "(no label)"
				}
				fmt.Fprintf(r.stderr, "  %s  %s  %s\n", ws.WorkspaceID, label, path)
			}
			return 2
		}
	}

	if opts.PromptFile != "" {
		b, err := os.ReadFile(opts.PromptFile)
		if err != nil {
			die(r.stderr, "prompt file not readable: "+opts.PromptFile)
			return 1
		}
		opts.Prompt = strings.TrimRight(string(b), "\n")
	}

	herderBin := r.paths.BinHerder
	spawnerPaneID := os.Getenv("HERDR_PANE_ID")
	spawnerTermID := ""
	if opts.Notify && opts.NotifyTo == "" && spawnerPaneID != "" {
		out, err := r.herdr.Output("pane", "get", spawnerPaneID)
		if err == nil {
			pane, parseErr := herdrcli.ParsePaneGet(out)
			if parseErr == nil {
				spawnerTermID = pane.TerminalID
			}
		}
	}
	permInjected := ""
	if !opts.Safe {
		if flag := defaultPermFlag(opts.Agent); flag != "" && !hasExplicitPermFlag(opts.ExtraArgs) {
			opts.ExtraArgs = append([]string{flag}, opts.ExtraArgs...)
			permInjected = flag
		}
	}

	stateDir := os.Getenv("HERDER_STATE_DIR")
	if stateDir == "" {
		stateBase := os.Getenv("XDG_STATE_HOME")
		if stateBase == "" {
			stateBase = filepath.Join(os.Getenv("HOME"), ".local", "state")
		}
		stateDir = filepath.Join(stateBase, "herder")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		die(r.stderr, err.Error())
		return 1
	}
	registryPath := filepath.Join(stateDir, "registry.jsonl")

	spawnedBy := os.Getenv("HERDER_GUID")
	if spawnedBy == "" {
		spawnedBy = "user"
	}

	// The child's bus dir (team or global) is resolved early because --notify-to
	// validation is scoped to the bus the CHILD will join; the team dir itself is
	// only created later, after the notify hard-error gate has passed.
	teamsRoot := os.Getenv("HERDER_TEAMS_ROOT")
	if teamsRoot == "" {
		teamsRoot = filepath.Join(os.Getenv("HOME"), ".hcom", "teams")
	}
	hcomDirEff := filepath.Join(os.Getenv("HOME"), ".hcom")
	if opts.Team != "" {
		hcomDirEff = filepath.Join(teamsRoot, opts.Team)
	}

	// Notify is bus-native ONLY (TASK-003): the spawner must resolve to a
	// recorded hcom name — via --notify-to (a registry row, or a bus name
	// checked against the registry and the child's bus), its own guid, or its
	// pane/terminal coordinates (enrolled sessions). The keystroke ring at a
	// terminal id went with the herdr delivery transport; a bus-less spawner is
	// a hard error BEFORE any pane is created, not a silent downgrade.
	if opts.Notify {
		name, ambiguous := resolveSpawnerBus(registryPath, opts.NotifyTo, spawnedBy, spawnerPaneID, spawnerTermID, hcomDirEff, r.stderr)
		switch {
		case name != "":
			opts.NotifyBusName = name
		case ambiguous:
			// A reused pane holds several seated sessions and bus liveness can't
			// single one out (TASK-035) — resolveSpawnerBus already warned with
			// the candidate list. Notify is best-effort (TASK-017
			// warn-never-block): drop it and spawn the worker anyway rather than
			// block real work or route a completion report to a guessed session.
			opts.Notify = false
		default:
			die(r.stderr, fmt.Sprintf("--notify: spawner does not resolve to a bus-bound agent (tried --notify-to %q as registry row and as bus name, spawner guid %q, pane %q, terminal %q) — the keystroke ring was removed; spawn from a bus-bound session, or point --notify-to at a bus-bound registry row or a live bus name on the child's bus", opts.NotifyTo, spawnedBy, spawnerPaneID, spawnerTermID))
			return 1
		}
	}

	guid, err := registry.NewGUID()
	if err != nil {
		die(r.stderr, err.Error())
		return 1
	}
	short := registry.ShortGUID(guid)
	label := spawnLabel(opts.Role, opts.LabelPrefix, short)
	grokSessionID := ""
	if opts.Agent == "grok" {
		grokSessionID, err = launchcmd.NewGrokSessionID()
		if err != nil {
			die(r.stderr, "preassign Grok session id: "+err.Error())
			return 1
		}
	}

	isHcomAgent := launchcmd.IsHcomCapable(opts.Agent)
	if isHcomAgent {
		if _, err := exec.LookPath("hcom"); err != nil {
			die(r.stderr, "hcom is required to spawn '"+opts.Agent+"' (launch-through-hcom); install hcom or spawn --agent bash")
			return 1
		}
	}

	if opts.Team != "" {
		_ = os.MkdirAll(hcomDirEff, 0o755)
	}

	// --worktree: drive `herdr worktree create` and spawn into the resulting
	// workspace in one verified step. The create opens a full WORKSPACE with a
	// seed tab + root shell pane; the payload's coordinates feed the guarded
	// seed-pane close after the agent starts. Everything herdr-side (branch,
	// checkout path, workspace) stays herdr-owned — herder only wraps it.
	var wtInfo *worktreeInfo
	rootPaneID, rootTerm := "", ""
	spawnCompleted := false
	if opts.Worktree != "" {
		// herdr refuses `worktree create` from inside a linked worktree, so
		// resolve the source checkout first: `worktree list --cwd` answers with
		// the parent repo for any dir inside the repo or one of its worktrees.
		spawnerCWD, _ := os.Getwd()
		srcOut, srcRC, _ := r.herdr.Combined("worktree", "list", "--cwd", spawnerCWD, "--json")
		src := parseWorktreeSource(srcOut)
		if srcRC != 0 || src == "" {
			fmt.Fprintf(r.stderr, "herder spawn: --worktree: cannot resolve a source repo from %s (herdr worktree list returned: %s)\n", spawnerCWD, strings.TrimRight(string(srcOut), "\n"))
			return 1
		}
		createArgs := []string{"worktree", "create", "--cwd", src, "--branch", opts.Worktree, "--no-focus", "--json"}
		if opts.Base != "" {
			createArgs = append(createArgs, "--base", opts.Base)
		}
		createOut, createRC, _ := r.herdr.Combined(createArgs...)
		created := parseWorktreeCreate(createOut)
		if createRC != 0 || created.WorkspaceID == "" || created.CheckoutPath == "" || created.TabID == "" {
			fmt.Fprintf(r.stderr, "herdr worktree create failed:\n%s\n", strings.TrimRight(string(createOut), "\n"))
			if createRC == 0 {
				createRC = 1
			}
			return createRC
		}
		wtInfo = &worktreeInfo{Branch: opts.Worktree, Base: opts.Base, CheckoutPath: created.CheckoutPath, WorkspaceID: created.WorkspaceID}
		opts.Workspace = created.WorkspaceID
		opts.Tab = created.TabID
		rootPaneID, rootTerm = created.RootPaneID, created.RootTerminalID
		// From here on the worktree EXISTS: any failed exit must say so loudly
		// (report, never auto-remove — the branch/checkout may be wanted, and a
		// destructive unwind on a half-understood failure is worse than a leak).
		defer func() {
			if spawnCompleted {
				return
			}
			fmt.Fprintf(r.stderr, "herder spawn: --worktree %s: worktree/workspace was CREATED but the spawn did not complete — left in place (not auto-removed):\n", opts.Worktree)
			fmt.Fprintf(r.stderr, "  workspace: %s\n", wtInfo.WorkspaceID)
			fmt.Fprintf(r.stderr, "  checkout:  %s\n", wtInfo.CheckoutPath)
			fmt.Fprintf(r.stderr, "  branch:    %s\n", wtInfo.Branch)
			fmt.Fprintf(r.stderr, "  reuse: herder spawn --workspace %s …  |  remove: herdr worktree remove --workspace %s --force (the git branch survives removal)\n", wtInfo.WorkspaceID, wtInfo.WorkspaceID)
		}()
	}

	childCWD := opts.CWD
	if wtInfo != nil {
		childCWD = wtInfo.CheckoutPath
	}
	if childCWD == "" && opts.Workspace != "" {
		if len(workspaces) == 0 && len(wsListOut) > 0 {
			workspaces = parseWorkspaces(wsListOut)
		}
		for _, ws := range workspaces {
			if ws.WorkspaceID == opts.Workspace {
				childCWD = firstNonEmpty(ws.Worktree.CheckoutPath, ws.Worktree.RepoRoot)
				break
			}
		}
	}
	if childCWD == "" {
		childCWD, _ = os.Getwd()
	}

	// Registry-write hygiene: herder lifecycle commands inside the child must
	// keep using the spawner's build, even when the child cwd is an ai-config
	// worktree at an older ref. bin/herder lets AI_CONFIG_ROOT override its own
	// location, so pin BOTH values to the spawner checkout; otherwise a child
	// hcom shim or an interactive `herder` command can build registry-writing
	// code from the child's checkout and append rows the live schema cannot
	// accept.
	childEnvBin := herderBin
	childEnvRoot := r.paths.RepoRoot

	launchTokens := []string{}
	if isHcomAgent {
		launchTokens = append(launchTokens, r.paths.BinHerder, "launch", opts.Agent, "--tag", opts.Role)
		launchTokens = append(launchTokens, opts.ExtraArgs...)
	} else {
		launchTokens = append(launchTokens, opts.Agent)
		launchTokens = append(launchTokens, opts.ExtraArgs...)
	}

	// Notify appendix: a finished worker pings the spawner so it needn't poll —
	// a real status message with content over the hcom bus, not a fixed slogan.
	// The bus name was resolved (and hard-error-checked) before pane creation.
	if opts.Notify && opts.Prompt != "" {
		opts.Prompt += fmt.Sprintf(`

When your unit is finished (or blocked), report to the spawner over the hcom bus so it does not have to poll — a real status message with content, not a fixed slogan:
  hcom send @%s --intent inform -- "<what you finished, what's left, any blockers>"
Send it ONCE when you are genuinely done or blocked, then end your turn. (If your hcom setup expects a --name, use your own agent name.)`, opts.NotifyBusName)
	}

	// hcom agents get HCOM_DIR (bus scoping) plus the shim dir prepended to PATH,
	// so the child's hcom traffic — the Claude hooks' `${HCOM:-hcom} <verb>` AND
	// the agent's own hcom CLI — resolves to our `hcom` shim, which forwards to
	// `herder hook`. That rewrites the sessionstart bootstrap to herder doctrine
	// and passes every other verb through verbatim. This is a PER-SPAWN PATH
	// scope, never machine-wide: the env-var vector (HCOM="herder hook") is dead
	// because hcom re-exports HCOM=hcom to its launch children, clobbering it.
	hcomEnv := ""
	if isHcomAgent {
		hcomEnv = " HCOM_DIR=" + shellquote.Quote(hcomDirEff) +
			" PATH=" + shellquote.Quote(r.paths.ShimsDir) + ":$PATH"
	}
	grokEnv := ""
	if opts.Agent == "grok" {
		grokEnv = " HERDER_STATE_DIR=" + shellquote.Quote(stateDir) +
			" HERDER_GROK_SESSION_ID=" + shellquote.Quote(grokSessionID) +
			" HERDER_GROK_PREASSIGNED=1"
		for _, key := range []string{"HERDER_REAL_HCOM"} {
			if value := os.Getenv(key); value != "" {
				grokEnv += " " + key + "=" + shellquote.Quote(value)
			}
		}
		if opts.Safe {
			grokEnv += " HERDER_GROK_SAFE=1"
		}
	}
	rootExport := " AI_CONFIG_ROOT=" + shellquote.Quote(childEnvRoot)
	argv := []string{}
	if opts.LoginShell {
		innerCmd := shellCommand(launchTokens)
		inner := fmt.Sprintf("%sexport HERDER_GUID=%s HERDER_ROLE=%s HERDER_LABEL=%s HERDER_SPAWNED_BY=%s HERDER_BIN=%s%s%s%s; exec %s",
			misePathFix, shellquote.Quote(guid), shellquote.Quote(opts.Role), shellquote.Quote(label), shellquote.Quote(spawnedBy), shellquote.Quote(childEnvBin), rootExport, hcomEnv, grokEnv, innerCmd)
		argv = []string{opts.LoginShellBin, "-lic", inner}
	} else {
		// The env form has no shell, so it gets the spawner herder pin but not
		// the mise shims PATH fix (that one needs runtime expansion).
		argv = []string{"env", "HERDER_GUID=" + guid, "HERDER_ROLE=" + opts.Role, "HERDER_LABEL=" + label, "HERDER_SPAWNED_BY=" + spawnedBy, "HERDER_BIN=" + childEnvBin}
		argv = append(argv, "AI_CONFIG_ROOT="+childEnvRoot)
		if isHcomAgent {
			argv = append(argv, "HCOM_DIR="+hcomDirEff, "PATH="+r.paths.ShimsDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
		if opts.Agent == "grok" {
			argv = append(argv, "HERDER_STATE_DIR="+stateDir, "HERDER_GROK_SESSION_ID="+grokSessionID, "HERDER_GROK_PREASSIGNED=1")
			for _, key := range []string{"HERDER_REAL_HCOM"} {
				if value := os.Getenv(key); value != "" {
					argv = append(argv, key+"="+value)
				}
			}
			if opts.Safe {
				argv = append(argv, "HERDER_GROK_SAFE=1")
			}
		}
		argv = append(argv, launchTokens...)
	}

	startArgs := []string{"agent", "start", label, opts.FocusFlag, "--split", opts.Split}
	if opts.Workspace != "" {
		startArgs = append(startArgs, "--workspace", opts.Workspace)
	}
	if opts.Tab != "" {
		startArgs = append(startArgs, "--tab", opts.Tab)
	}
	// Place the agent in the resolved cwd — the explicit --cwd, else the anchored
	// workspace's checkout path, else the spawner's own cwd (os.Getwd). Passing it
	// explicitly is what makes "default: current" true: without a --cwd on the
	// wire, herdr starts the child in its own default (e.g. $HOME), which for a
	// fresh/untrusted dir re-opens the trust modal. childCWD is only "" if getwd
	// itself failed, in which case we let herdr pick as before.
	if childCWD != "" {
		startArgs = append(startArgs, "--cwd", childCWD)
	}
	startArgs = append(startArgs, "--")
	startArgs = append(startArgs, argv...)

	startOut, startRC, _ := r.herdr.Combined(startArgs...)
	if startRC != 0 {
		fmt.Fprintf(r.stderr, "herdr agent start failed:\n%s\n", strings.TrimRight(string(startOut), "\n"))
		return startRC
	}
	start, err := parseAgentStart(startOut)
	if err != nil || start.Agent.PaneID == "" {
		fmt.Fprintf(r.stderr, "unexpected start payload: %s\n", strings.TrimRight(string(startOut), "\n"))
		return 1
	}
	paneID := start.Agent.PaneID
	wsID := start.Agent.WorkspaceID
	tabID := start.Agent.TabID
	termID := start.Agent.TerminalID
	resolvedCWD := start.Agent.CWD
	launchPaneID := paneID
	newTabResult := ""

	if opts.NewTab {
		moveArgs := newTabMoveArgs(paneID, label, opts.FocusFlag)
		if out, rc, _ := r.herdr.Combined(moveArgs...); rc == 0 {
			newTabResult = "moved"
		} else {
			reason := compactMessage(string(out))
			if reason == "" {
				reason = fmt.Sprintf("herdr pane move exited %d", rc)
			}
			newTabResult = "move_failed: " + reason
			return r.failAfterLaunch("fresh-tab placement failed: "+reason, paneID, termID)
		}
		if out, err := r.herdr.Output("pane", "get", paneID); err == nil {
			if pane, parseErr := herdrcli.ParsePaneGet(out); parseErr == nil {
				paneID = firstNonEmpty(pane.PaneID, paneID)
				wsID = firstNonEmpty(pane.WorkspaceID, wsID)
				tabID = firstNonEmpty(pane.TabID, tabID)
				termID = firstNonEmpty(pane.TerminalID, termID)
			}
		}
		opts.Tab = tabID
	}

	// Seed-pane close: --worktree's workspace create leaves a root shell pane
	// behind; --new-tab now moves the running agent pane and never creates a
	// seed shell.
	rootClosed := false
	if rootPaneID != "" && rootTerm != "" {
		if rootTerm == termID {
			fmt.Fprintf(r.stderr, "herder spawn: refusing to close root pane — terminal_id matches the agent (%s)\n", termID)
		} else {
			out, err := r.herdr.Output("pane", "get", rootPaneID)
			liveRootTerm := ""
			if err == nil {
				pane, parseErr := herdrcli.ParsePaneGet(out)
				if parseErr == nil {
					liveRootTerm = pane.TerminalID
				}
			}
			if liveRootTerm == rootTerm {
				if rc, _ := r.herdr.Run("pane", "close", rootPaneID); rc == 0 {
					rootClosed = true
				}
			} else {
				now := liveRootTerm
				if now == "" {
					now = "gone"
				}
				fmt.Fprintf(r.stderr, "herder spawn: skipped root-pane close — %s no longer holds terminal %s (now %s)\n", rootPaneID, rootTerm, now)
			}
		}
		if rootClosed {
			if out, err := r.herdr.Output("pane", "list"); err == nil {
				if panes, parseErr := herdrcli.ParsePaneList(out); parseErr == nil {
					for _, pane := range panes {
						if pane.TerminalID == termID {
							paneID = pane.PaneID
							break
						}
					}
				}
			}
		}
	}

	// Initial-prompt delivery is bus-first (TASK-032): a bus-capable agent's
	// prompt waits for the child to BIND its bus name, then rides hcom with a
	// receipt-based verify — the boot-paste engine (TUI readiness scraping,
	// Enter retries) no longer touches these families. hcom wakes an idle,
	// empty-composer agent instantly (even a never-prompted fresh one) and
	// holds a message sent mid-boot until the session is deliverable, so the
	// send fires at the earliest bind instant with no TUI-ready gate. Paste
	// remains for bash (no bus binding ever exists to ride).
	busPrompt := isHcomAgent && opts.Prompt != ""
	readyReason := ""
	trustBlocked := false
	modalCleared := false
	capturedName := ""
	switch {
	case busPrompt || opts.Agent == "grok":
		// Bind is the delivery gate, so --no-ready-wait cannot skip this wait
		// (ruling: it stays meaningful only for the paste path). The trust
		// modal blocks BOOT itself — pre-bind — so awaitBind clears it too.
		capturedName, readyReason, trustBlocked, modalCleared = r.awaitBind(&paneID, registryPath, guid, hcomDirEff, launchPaneID, grokSessionID)
		_ = modalCleared
	case opts.NoReadyWait:
		readyReason = "ready-wait skipped (--no-ready-wait)"
	default:
		readyReason, trustBlocked, modalCleared = r.awaitReady(&paneID)
		sleepMS(opts.SettleMS)
		_ = modalCleared
	}
	if code := r.failUnboundGrok(capturedName, readyReason, paneID, termID); code != 0 {
		return code
	}

	promptSent := false
	deliveryResult := "not_attempted"
	pasteNotes := []string(nil)
	if opts.Prompt != "" && trustBlocked {
		deliveryResult = "blocked_trust_modal"
	} else if busPrompt {
		if capturedName != "" {
			deliveryResult = send.DeliverBus(capturedName, hcomDirEff, opts.Prompt, opts.VerifyMS)
			if deliveryResult == "delivered" || deliveryResult == "queued" {
				promptSent = true
			}
		} else if strings.HasPrefix(readyReason, "bound-but-ready-match-timeout") {
			deliveryResult = "ready_match_timeout"
		} else if strings.HasPrefix(readyReason, "launch-refused: ") {
			deliveryResult = "launch_refused"
		} else {
			deliveryResult = "bind_timeout"
		}
	} else if opts.Prompt != "" {
		paste := (&bootPaster{Client: r.herdr}).paste(paneID, opts.Prompt)
		if paste.Verify != "" {
			deliveryResult = paste.Verify
		}
		if paste.Code == 0 {
			promptSent = true
		}
		if paste.ComposerCleared {
			pasteNotes = append(pasteNotes, "composer_cleared")
		}
	}
	if opts.Agent == "grok" && strings.HasPrefix(readyReason, "launch-refused: ") {
		deliveryResult = "launch_refused"
	}
	if deliveryResult == "launch_refused" {
		_ = panecleanup.CloseConfirmed(r.herdr, paneID, termID)
		die(r.stderr, strings.TrimPrefix(readyReason, "launch-refused: "))
		return 1
	}

	hcomDirRec, hcomTagRec := "", ""
	if isHcomAgent {
		hcomDirRec = hcomDirEff
		hcomTagRec = opts.Role
	}
	provenance := registry.BuildProvenance("spawn", spawnedBy, opts.Role, resolvedCWD, wsID)
	if grokSessionID != "" {
		provenance.ToolSessionID = grokSessionID
	}
	record := spawnRecord{
		GUID:                 guid,
		ShortGUID:            short,
		Label:                label,
		Role:                 opts.Role,
		Agent:                opts.Agent,
		ExtraArgs:            recordExtraArgs(opts.ExtraArgs),
		Argv:                 argv,
		PaneID:               paneID,
		WorkspaceID:          wsID,
		TabID:                tabID,
		TerminalID:           termID,
		CWD:                  resolvedCWD,
		StartedAt:            time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		StartedByPane:        firstNonEmpty(os.Getenv("HERDR_PANE_ID"), "unknown"),
		InitialPromptPresent: opts.Prompt != "",
		Team:                 opts.Team,
		HcomDir:              hcomDirRec,
		HcomName:             capturedName,
		HcomTag:              hcomTagRec,
		Status:               "active",
		Provenance:           provenance,
	}
	if code := r.registerSpawnOrRollback(registryPath, record); code != 0 {
		return code
	}

	hcomCapture := "not_hcom_agent"
	if isHcomAgent && record.HcomName != "" {
		// Bus-first delivery already bound the name (awaitBind) and the row
		// above records it — no post-write capture loop to run.
		hcomCapture = "captured"
	} else if isHcomAgent {
		// Post-write row enrichment trusts CHILD-SPECIFIC signals ONLY, the same
		// discipline the bus-first bind gate enforces: this guid's sidecar registry
		// enrichment, Grok's live bridge status operation, or (for other families)
		// the hcom roster entry whose launch_context matches the frozen launch pane.
		// The tag+cwd-unique
		// fallback is GONE (TASK-033): even a UNIQUE same-tag+cwd match can be a
		// STALE pre-existing agent still on the bus, and enriching the row with
		// its name would make a later `herder send <guid>` message the WRONG
		// session — no prompt misdelivery (that gate is already fixed), but a
		// mislabeled row. When no child-specific signal appears the name is LEFT
		// EMPTY for sidecar enrichment to fill from the child's own pane row
		// (findRowForPane) later — never guessed.
		hcomCapture = "not_found"
		if name := registryCapturedName(registryPath, guid); opts.Agent != "grok" && name != "" {
			// The sidecar already persisted this enrichment to the registry; the
			// in-memory record just needs the name for the summary/JSON. No second
			// append.
			record.HcomName = name
			hcomCapture = "captured"
		} else {
			for i := 0; i < 6; i++ {
				name := ""
				if opts.Agent == "grok" {
					name = grokBoundBusOnce(filepath.Dir(registryPath), guid, grokSessionID)
				} else {
					for _, entry := range hcomList(hcomDirEff) {
						if entry.LaunchContext.PaneID == launchPaneID {
							name = entry.Name
							break
						}
					}
				}
				if name != "" {
					record.HcomName = name
					hcomCapture = "captured"
					outcomes, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
						current := registry.V2ByGUID(tx.Projection, record.GUID)
						if current == nil || current.State == v2.StateRetired || current.State == v2.StateLost {
							return nil, nil
						}
						next := *current
						next.Event = "recognised"
						next.State = v2.StateSeated
						next.RecordedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
						if next.Seat == nil {
							next.Seat = &v2.Seat{Kind: "herdr"}
						}
						next.Seat.HcomName = name
						next.Seat.ConfirmedAt = next.RecordedAt
						return []v2.SessionRecord{next}, nil
					})
					if err == nil && len(outcomes) > 0 {
						var outcome registry.WriteOutcome
						outcome, err = registry.SingleOutcome(outcomes)
						if err == nil {
							err = outcome.Err()
						}
					}
					if err != nil {
						die(r.stderr, err.Error())
						return 1
					}
					break
				}
				sleepMS(700)
			}
		}
	}

	r.writeSummary(record, wtInfo, isHcomAgent, rootClosed, newTabResult, permInjected, hcomCapture, busPrompt, promptSent, deliveryResult, readyReason, trustBlocked, pasteNotes)
	if opts.JSONOutput {
		outRecord := spawnJSONRecord{
			GUID:                 record.GUID,
			ShortGUID:            record.ShortGUID,
			Label:                record.Label,
			Role:                 record.Role,
			Agent:                record.Agent,
			ExtraArgs:            record.ExtraArgs,
			Argv:                 record.Argv,
			PaneID:               record.PaneID,
			WorkspaceID:          record.WorkspaceID,
			TabID:                record.TabID,
			TerminalID:           record.TerminalID,
			CWD:                  record.CWD,
			StartedAt:            record.StartedAt,
			StartedByPane:        record.StartedByPane,
			InitialPromptPresent: record.InitialPromptPresent,
			Team:                 record.Team,
			HcomDir:              record.HcomDir,
			HcomName:             record.HcomName,
			HcomTag:              record.HcomTag,
			Status:               record.Status,
			Provenance:           record.Provenance,
			PromptSent:           promptSent,
			DeliveryResult:       deliveryResult,
			ResendCommand:        resendCommandFor(deliveryResult, record.Label, opts.Prompt),
			PasteNotes:           pasteNotes,
			PermInjected:         permInjected,
			NewTab:               opts.NewTab,
			RootPaneClosed:       rootClosed,
			NewTabResult:         newTabResult,
			HcomCapture:          hcomCapture,
			Worktree:             wtInfo,
		}
		b, _ := json.Marshal(outRecord)
		fmt.Fprintln(r.stdout, string(b))
	}
	spawnCompleted = true
	observercmd.NudgeIfConfigured(r.stderr)
	return 0
}

func (r *runner) awaitReady(paneID *string) (reason string, trustBlocked bool, modalCleared bool) {
	prev := ""
	waited := 0
	status := ""
	lastVisible := ""
	for waited < r.opts.WaitTimeoutMS {
		// recent-unwrapped stays the basis for the readiness/stability compare
		// below (normal composer content lands there). But the first-run trust
		// dialog is an alternate-screen overlay: it renders on the VISIBLE
		// screen yet never enters the scrollback stream, so recent-unwrapped is
		// null while it is up. Match the modal regex against BOTH sources so
		// detection is not blind to the overlay.
		text := r.paneText(*paneID)
		visible := r.paneVisibleText(*paneID)
		lastVisible = visible
		status = r.paneStatus(*paneID)
		if trustModalRE.MatchString(text) || trustModalRE.MatchString(visible) {
			if r.opts.Safe {
				return "trust-modal-open (blocked by --safe; accept it in the pane)", true, modalCleared
			}
			_, _ = r.herdr.Run("pane", "send-keys", *paneID, "Enter")
			modalCleared = true
			prev = ""
			sleepMS(800)
			waited += 800
			continue
		}
		if (status == "idle" || status == "done") && (r.opts.ReadyMatch == "" || strings.Contains(text, r.opts.ReadyMatch)) {
			if prev != "" && text == prev {
				suffix := ""
				if modalCleared {
					suffix = ",trust-accepted"
				}
				return "status=" + status + ",stable" + suffix, false, modalCleared
			}
			prev = text
		}
		sleepMS(500)
		waited += 500
	}
	if status == "" {
		status = "unknown"
	}
	suffix := ""
	if modalCleared {
		suffix = ",trust-accepted"
	}
	reason = "timeout(status=" + status + suffix + ")"
	// Self-healing: a wedged pane (classically status=blocked) whose overlay we
	// could not match is an UNKNOWN modal. Surface the first visible line so the
	// caller sees WHAT is blocking instead of a bare status=blocked timeout.
	if status == "blocked" {
		if snippet := firstVisibleLine(lastVisible); snippet != "" {
			reason += " blocked-by: " + snippet
		}
	}
	return reason, false, modalCleared
}

// awaitBind waits for the child to BIND its bus name — the delivery gate for
// bus-first initial prompts (TASK-032). Bind is positively observable via
// CHILD-SPECIFIC signals only (childBoundBusOnce: this guid's registry
// enrichment, or the frozen-launch-pane roster match) and lands early in
// boot, well before the TUI is interactive — hcom holds a message sent at
// that instant until the session is deliverable, so no TUI-readiness gate is
// layered on top. The
// trust modal is the one boot blocker that precedes bind, so it is cleared
// here exactly as in awaitReady (--safe refuses instead). --ready-match,
// when given, additionally gates the send on the pane text (ruling: the flag
// keeps its "don't deliver before the screen shows X" meaning on both paths).
// Budget: HERDER_SPAWN_BIND_MS (default 60000). Claude/bash publish
// launch_context.pane_id, so the roster match here resolves them in a second or
// two; codex omits pane_id and is only correlated via the sidecar's async
// tag+cwd registry enrichment, which under load can lag past any window
// (TASK-036) — a codex bind_timeout is expected, and its recovery is the exact
// verbatim resend command reported below.
func (r *runner) awaitBind(paneID *string, registryPath, guid, hcomDir, launchPaneID, grokSessionID string) (name, reason string, trustBlocked, modalCleared bool) {
	waited := 0
	boundName := ""
	for waited < r.opts.BindTimeoutMS {
		if r.opts.Agent == "grok" {
			if failure := launchcmd.ReadGrokLaunchFailure(filepath.Dir(registryPath), guid); failure != "" {
				return "", "launch-refused: " + failure, false, modalCleared
			}
		}
		text := r.paneText(*paneID)
		visible := r.paneVisibleText(*paneID)
		if trustModalRE.MatchString(text) || trustModalRE.MatchString(visible) {
			if r.opts.Safe {
				return "", "trust-modal-open (blocked by --safe; accept it in the pane)", true, modalCleared
			}
			_, _ = r.herdr.Run("pane", "send-keys", *paneID, "Enter")
			modalCleared = true
			sleepMS(800)
			waited += 800
			continue
		}
		if boundName == "" {
			if r.opts.Agent == "grok" {
				boundName = grokBoundBusOnce(filepath.Dir(registryPath), guid, grokSessionID)
			} else {
				boundName = childBoundBusOnce(registryPath, guid, hcomDir, launchPaneID)
			}
		}
		if boundName != "" && (r.opts.ReadyMatch == "" || strings.Contains(text, r.opts.ReadyMatch)) {
			return boundName, "bound" + trustSuffix(modalCleared), false, modalCleared
		}
		sleepMS(500)
		waited += 500
	}
	if boundName != "" {
		// Bound, but --ready-match never showed: honor the caller's gate — no
		// send — and say exactly which half timed out.
		return "", "bound-but-ready-match-timeout(" + strconv.Itoa(r.opts.BindTimeoutMS) + "ms)" + trustSuffix(modalCleared), false, modalCleared
	}
	reason = "bind-timeout(" + strconv.Itoa(r.opts.BindTimeoutMS) + "ms)" + trustSuffix(modalCleared)
	// Self-healing (same rule as awaitReady): a wedged pane whose overlay we
	// could not match is an UNKNOWN modal — it can block boot before the bus
	// bind ever happens. Surface the first visible line so the caller sees
	// WHAT is blocking instead of a bare bind timeout.
	if r.paneStatus(*paneID) == "blocked" {
		if snippet := firstVisibleLine(r.paneVisibleText(*paneID)); snippet != "" {
			reason += " blocked-by: " + snippet
		}
	}
	return "", reason, false, modalCleared
}

func (r *runner) failUnboundGrok(name, reason, paneID, terminalID string) int {
	if r.opts.Agent != "grok" || name != "" || !strings.HasPrefix(reason, "bind-timeout") {
		return 0
	}
	return r.failAfterLaunch(
		"Grok bridge did not report a live bound bus before "+reason+
			"; inspect the seat bridge log under HERDER_STATE_DIR/grok/<seat>/bridge.log, correct the bridge or hcom configuration, then retry the spawn",
		paneID,
		terminalID,
	)
}

func trustSuffix(modalCleared bool) string {
	if modalCleared {
		return ",trust-accepted"
	}
	return ""
}

func (r *runner) paneText(paneID string) string {
	return r.paneRead(paneID, "recent-unwrapped")
}

func (r *runner) paneVisibleText(paneID string) string {
	return r.paneRead(paneID, "visible")
}

func (r *runner) paneRead(paneID, source string) string {
	out, err := r.herdr.Output("agent", "read", paneID, "--source", source, "--lines", "40")
	if err != nil {
		return ""
	}
	var envelope struct {
		Result struct {
			Read struct {
				Text string `json:"text"`
			} `json:"read"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &envelope) != nil {
		return ""
	}
	return envelope.Result.Read.Text
}

// firstVisibleLine returns the first non-blank line of a visible-screen capture,
// trimmed and length-capped, for surfacing an unrecognized blocking overlay in a
// timeout reason.
func firstVisibleLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if runes := []rune(s); len(runes) > 80 {
			s = string(runes[:80])
		}
		return s
	}
	return ""
}

func (r *runner) paneStatus(paneID string) string {
	out, err := r.herdr.Output("agent", "list")
	if err != nil {
		return ""
	}
	agents, err := herdrcli.ParseAgentList(out)
	if err != nil {
		return ""
	}
	for _, agent := range agents {
		if agent.PaneID == paneID {
			return agent.Status
		}
	}
	return ""
}

func (r *runner) writeSummary(record spawnRecord, wtInfo *worktreeInfo, isHcomAgent, rootClosed bool, newTabResult, permInjected, hcomCapture string, busPrompt, promptSent bool, deliveryResult, readyReason string, trustBlocked bool, pasteNotes []string) {
	fmt.Fprintf(r.stderr, "spawned %s (%s) in pane %s (workspace %s)\n", record.Label, record.Agent, record.PaneID, record.WorkspaceID)
	fmt.Fprintf(r.stderr, "  guid:   %s\n", record.GUID)
	fmt.Fprintf(r.stderr, "  cwd:    %s\n", record.CWD)
	if wtInfo != nil {
		base := ""
		if wtInfo.Base != "" {
			base = " (base " + wtInfo.Base + ")"
		}
		fmt.Fprintf(r.stderr, "  worktree: branch %s%s @ %s\n", wtInfo.Branch, base, wtInfo.CheckoutPath)
		if rootClosed {
			fmt.Fprintf(r.stderr, "            workspace %s (new; seed shell closed, agent is sole pane) — remove later: herdr worktree remove --workspace %s\n", wtInfo.WorkspaceID, wtInfo.WorkspaceID)
		} else {
			fmt.Fprintf(r.stderr, "            workspace %s (new; WARNING: seed shell NOT closed — spare pane may remain) — remove later: herdr worktree remove --workspace %s\n", wtInfo.WorkspaceID, wtInfo.WorkspaceID)
		}
		fmt.Fprintf(r.stderr, "            after cull the workspace auto-closes (herdr remove no longer applies); then: git worktree remove %s && git branch -D %s\n", wtInfo.CheckoutPath, wtInfo.Branch)
	}
	if r.opts.NewTab {
		if strings.HasPrefix(newTabResult, "move_failed:") {
			fmt.Fprintf(r.stderr, "  tab:    %s (new-tab move FAILED; agent remains alive in this tab: %s)\n", record.TabID, strings.TrimPrefix(newTabResult, "move_failed: "))
		} else {
			fmt.Fprintf(r.stderr, "  tab:    %s (new; agent pane moved, no seed shell)\n", record.TabID)
		}
	}
	if permInjected != "" {
		fmt.Fprintf(r.stderr, "  perms:  %s (autonomous; pass --safe to opt out)\n", permInjected)
	} else if r.opts.Safe {
		fmt.Fprintln(r.stderr, "  perms:  --safe (agent default ask-mode)")
	}
	if r.opts.Notify {
		fmt.Fprintf(r.stderr, "  notify: worker reports to @%s over hcom on done\n", r.opts.NotifyBusName)
	}
	if isHcomAgent {
		if hcomCapture == "captured" {
			fmt.Fprintf(r.stderr, "  bus:    %s @%s  (team: %s)\n", firstNonEmpty(record.Team, "global"), record.HcomName, teamSummary(record.Team))
			fmt.Fprintf(r.stderr, "          HCOM_DIR=%s\n", record.HcomDir)
		} else {
			fmt.Fprintf(r.stderr, "  bus:    %s — name NOT captured (%s); herder send cannot reach it (bus-only) — inspect the pane directly\n", firstNonEmpty(record.Team, "global"), hcomCapture)
			fmt.Fprintf(r.stderr, "          HCOM_DIR=%s\n", record.HcomDir)
		}
	} else {
		fmt.Fprintf(r.stderr, "  bus:    n/a (%s is not an hcom agent — no bus name, so herder send cannot reach it)\n", record.Agent)
	}
	if r.opts.Prompt != "" {
		noteSuffix := pasteNoteSuffix(pasteNotes)
		switch {
		case busPrompt && promptSent && deliveryResult == "delivered":
			fmt.Fprintf(r.stderr, "  prompt: sent (%d chars) over the hcom bus, bind: %s, verify: delivered (receipt seen)\n", len(r.opts.Prompt), readyReason)
		case busPrompt && promptSent:
			fmt.Fprintf(r.stderr, "  prompt: sent (%d chars) over the hcom bus, bind: %s, verify: %s\n", len(r.opts.Prompt), readyReason, deliveryResult)
			fmt.Fprintln(r.stderr, "          no receipt in the window — it injects when the agent is deliverable; do NOT resend.")
			fmt.Fprintf(r.stderr, "          If it never lands: UNSUBMITTED COMPOSER TEXT starves bus delivery — check `herder wait %s --read`;\n", record.Label)
			fmt.Fprintln(r.stderr, "          a queued bus message visible on the input line is NOT garbage — do NOT clear it.")
			fmt.Fprintf(r.stderr, "          Clear only unrelated garbage text: herdr pane send-keys %s ctrl+u\n", record.PaneID)
		case promptSent:
			fmt.Fprintf(r.stderr, "  prompt: sent (%d chars), ready: %s, verify: %s%s\n", len(r.opts.Prompt), readyReason, deliveryResult, noteSuffix)
		case deliveryResult == "blocked_trust_modal":
			fmt.Fprintln(r.stderr, "  prompt: NOT sent — a directory-trust modal is open and --safe forbids auto-accepting it.")
			fmt.Fprintf(r.stderr, "          Accept it in the pane (focus + Enter), then: herder send %s \"<prompt>\"\n", record.Label)
		case deliveryResult == "bind_timeout" || deliveryResult == "ready_match_timeout":
			fmt.Fprintf(r.stderr, "  prompt: NOT sent (%s) — nothing went on the wire; a resend is SAFE.\n", readyReason)
			fmt.Fprintf(r.stderr, "          once `herder list` shows its bus name, resend verbatim (also in --json as resend_command):\n")
			fmt.Fprintf(r.stderr, "          %s\n", resendCommand(record.Label, r.opts.Prompt))
		case busPrompt:
			fmt.Fprintf(r.stderr, "  prompt: NOT confirmed (verify: %s, bind: %s) — the bus send did not go through\n", deliveryResult, readyReason)
			fmt.Fprintf(r.stderr, "          check the bus first (`hcom events --agent %s`), then retry: herder send %s \"<prompt>\"\n", record.HcomName, record.Label)
		default:
			fmt.Fprintf(r.stderr, "  prompt: NOT confirmed (verify: %s, ready: %s%s) — delivery unverified\n", deliveryResult, readyReason, noteSuffix)
			fmt.Fprintf(r.stderr, "          read the pane first: herder wait %s --read; do NOT blind-resend (double-submits);\n", record.Label)
			fmt.Fprintf(r.stderr, "          if garbage text sits stranded on the input line, clear it: herdr pane send-keys %s ctrl+u\n", record.PaneID)
		}
	} else if trustBlocked {
		fmt.Fprintln(r.stderr, "  note:   directory-trust modal is open (--safe); accept it in the pane to use the agent")
	}
}

func pasteNoteSuffix(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	return " (notes: " + strings.Join(notes, ",") + ")"
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder spawn: %s\n", msg)
}

func printHelp(stdout io.Writer) {
	lines := []string{
		"herder spawn — spawn a named, GUID-tagged agent in a herdr pane and register it.",
		"",
		"Usage:",
		"  herder spawn --role <role> --agent <claude|codex|bash|...> [--prompt TEXT | --prompt-file FILE]",
		"               [--team NAME] [--split right|down] [--workspace ID | --from-pane PANE_ID]",
		"               [--tab ID | --new-tab | --worktree BRANCH [--base REF]] [--cwd PATH] [--safe]",
		"               [--notify | --notify-to TARGET] [--model ID] [--extra-arg ARG]... [--focus] [--json]",
		"",
		"Options:",
		"  --role R          agent role; becomes the hcom --tag and label prefix (required)",
		"  --agent A         tool to run: claude, codex, gemini, bash, ... (required)",
		"  --prompt TEXT     initial prompt (or --prompt-file F): bus-capable agents get it as a",
		"                    verified hcom message once their bus name binds; bash gets it typed",
		"  --team NAME       join the bus at $HERDER_TEAMS_ROOT/<NAME> (default: global ~/.hcom)",
		"  --split D         opt into the current tab with a right or down split",
		"  --workspace ID    place in this workspace; --from-pane PANE_ID copies another pane's",
		"  --tab ID          add to an existing tab; --new-tab explicitly selects the default fresh-tab placement",
		"  --worktree BRANCH create a fresh git worktree on BRANCH (via `herdr worktree create`) and",
		"                    spawn into its workspace in one step; --base REF picks the start point",
		"  --cwd PATH        working directory for the agent (default: current)",
		"  --safe            keep the agent's default ask-mode (default: autonomous/skip-permissions)",
		"  --focus           focus the new pane or tab (default: keep the current pane focused)",
		"  --notify          worker reports to the spawner over the hcom bus on done (requires --prompt",
		"                    and a bus-bound spawner); --notify-to TARGET resolves the bus name from",
		"                    another registry row (guid, label, terminal_id, pane_id, or recorded hcom",
		"                    name), else accepts TARGET as a literal bus name if live on the child's bus",
		"  --extra-arg ARG   pass ARG through to the agent (repeatable)",
		"  --model ID        pin the model for claude, codex, or grok",
		"  --json            print the registry record as JSON on stdout",
		"",
		"Advanced:",
		"  --label-prefix STR    override the label prefix (default: the role)",
		"  --no-login-shell      run the agent without a login+interactive shell wrapper",
		"  --wait-timeout-ms MS  paste path (bash) / promptless spawns: max boot ready-wait",
		"                        (bus delivery waits for hcom BIND instead: HERDER_SPAWN_BIND_MS,",
		"                        default 60000; receipt window: HERDER_SPAWN_VERIFY_MS, default 20000)",
		"  --ready-match STR     don't deliver before the pane's screen matches STR (both paths)",
		"  --no-ready-wait       paste path/promptless only: skip the boot ready-wait. Bus delivery",
		"                        cannot skip its bind wait — without a bound bus name there is",
		"                        nothing to deliver to.",
		"",
		"Behavior:",
		"  Non-worktree spawns open in a fresh tab by default. Pass --split right|down to opt",
		"  into splitting the target workspace's current tab. --workspace chooses the work",
		"  workspace directly; otherwise the caller pane anchors placement so UI focus cannot",
		"  redirect the spawn into an unrelated workspace.",
		"",
		"  claude/codex/gemini launch THROUGH hcom (via `herder launch`) so they bind to the",
		"  message bus from birth — hcom is a HARD dependency for them; other agents exec raw",
		"  and get no bus name, so `herder send` (bus-only) cannot reach them after spawn.",
		"  The assigned bus name, team, and hcom coordinates are captured into the registry",
		"  so send/wait/cull can resolve this agent.",
		"",
		"  A fresh/untrusted directory opens claude's first-run trust dialog before the",
		"  agent is receptive. Autonomous mode auto-accepts it (the dialog is an",
		"  alternate-screen overlay, so detection reads the pane's VISIBLE source);",
		"  --safe leaves it up and surfaces it so you can accept it in the pane.",
		"",
		"  Initial-prompt delivery rides the hcom bus: spawn waits for the child to",
		"  BIND its bus name (early in boot, well before the TUI is interactive), sends the full",
		"  prompt as a bus message, and reports the receipt — verify: delivered (receipt seen) or",
		"  queued (sent, no receipt yet; it injects the moment the agent is deliverable — do NOT",
		"  resend). On bind_timeout nothing goes on the wire (a resend is SAFE): the summary and",
		"  --json (resend_command) carry the exact verbatim `herder send` command to run once the",
		"  bus name shows in `herder list` — codex correlates via a slower path, so it hits this",
		"  more often. hcom wakes an idle agent with an EMPTY composer instantly, even a fresh",
		"  never-prompted one; a message sent mid-boot is held until the session can take it.",
		"  The one thing that starves bus delivery — on both families — is UNSUBMITTED TEXT in",
		"  the composer: nothing injects until it is submitted or cleared. Preferred remedy for",
		"  stray/garbage text: `herdr pane send-keys <pane> ctrl+u`; use Enter only when the",
		"  visible text is a real message you intend to submit.",
		"  A slash-command prompt (e.g. --prompt '/review …') arrives as MESSAGE TEXT, not as a",
		"  typed slash command — the agent can invoke the skill itself, but nothing auto-executes.",
		"  bash agents have no bus: their prompt is typed into the pane by the spawn-private",
		"  paste engine and verified on-screen (the paste engine's other user is herder compact).",
		"",
		"  --worktree wraps `herdr worktree create` (worktree/workspace lifecycle stays herdr-owned):",
		"  the source repo resolves from the spawner's cwd (works from inside a linked worktree),",
		"  the created workspace's seed shell pane is closed under an identity guard, and the",
		"  summary + --json surface the worktree coordinates (workspace_id,",
		"  checkout path, branch) for later lifecycle management. If the worktree is created but the",
		"  spawn then fails, NOTHING is auto-removed — the failure report names the workspace and the",
		"  exact `herdr worktree remove` command. The workspace label stays herdr's branch-derived",
		"  default (it names the TREE); the agent label stays role-short (it names the AGENT).",
		"  Cleanup: `herdr worktree remove --workspace <id>` works only while the workspace is open.",
		"  Culling the workspace's last agent auto-closes it, leaving the git worktree + branch on",
		"  disk — from there use `git worktree remove <checkout_path> && git branch -D <branch>`",
		"  (the summary prints this breadcrumb with the real coordinates).",
		"",
		"  --notify is the doorbell: a finished worker reports to the spawner over the hcom bus so",
		"  it needn't poll wait in a loop. The spawner's bus name resolves from the registry by its",
		"  guid AND by its pane/terminal coordinates, so enrolled sessions (no $HERDER_GUID in their",
		"  environment) route bus-native too. --notify-to may also name the target directly by its",
		"  bus name: a seated registry session's hcom_name matches, and an unregistered name is accepted",
		"  if live on the bus the child will join (team-scoped — cross-bus names still refuse).",
		"  A spawner that resolves to NO bus name is a hard error (the keystroke ring was removed).",
		"  It is only a signal — send it once and stop.",
		"",
		"  Child environment: the SPAWNING checkout's tools/herder/shims dir is prepended to the",
		"  child's PATH (hcom agents), so spawning from a worktree injects THAT worktree's shims —",
		"  by design; sibling shims recognize each other by marker and never loop. The login-shell",
		"  form also pins mise's shims dir to the front of PATH, so mise-managed toolchains beat",
		"  system ones even though rc-file mise activation is prompt-hook driven and inert in a",
		"  spawned pane. AI_CONFIG_ROOT and HERDER_BIN are pinned to the SPAWNER checkout even",
		"  when --cwd/--worktree lands the child in another ai-config checkout: herder lifecycle",
		"  commands write the shared registry, so they must use the same schema generation as the",
		"  spawner build rather than an older child worktree. To deliberately run herder from the",
		"  child checkout, override the pin explicitly: `env -u AI_CONFIG_ROOT -u HERDER_BIN",
		"  ./bin/herder ...` or `AI_CONFIG_ROOT=$PWD ./bin/herder ...`.",
		"",
		"  --team caveat: team-bus launches pin claude's config dir and seed its state from",
		"  ~/.claude.json, so an onboarded machine skips claude's one-time onboarding; only a",
		"  never-onboarded machine (no ~/.claude.json) sees it once in the pane, and it persists",
		"  machine-wide after that. The global bus pins nothing and is unaffected.",
	}
	fmt.Fprint(stdout, strings.Join(lines, "\n")+"\n")
}

func defaultPermFlag(agent string) string {
	switch agent {
	case "claude":
		return "--dangerously-skip-permissions"
	case "codex":
		return "--dangerously-bypass-approvals-and-sandbox"
	case "grok":
		return "--always-approve"
	default:
		return ""
	}
}

func hasExplicitPermFlag(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--dangerously-skip-permissions", "--permission-mode", "--permission-prompt-tool",
			"--dangerously-bypass-approvals-and-sandbox", "--full-auto", "-a", "--ask-for-approval", "-s", "--sandbox", "--always-approve":
			return true
		}
	}
	return false
}

func recordExtraArgs(args []string) []string {
	if len(args) == 0 {
		return []string{""}
	}
	return args
}

func hasModelExtraArg(agent string, args []string) bool {
	for i, arg := range args {
		if arg == "--model" || arg == "-m" || strings.HasPrefix(arg, "--model=") {
			return true
		}
		if agent != "codex" {
			continue
		}
		switch {
		case (arg == "-c" || arg == "--config") && i+1 < len(args):
			if isModelConfigOverride(args[i+1]) {
				return true
			}
		case strings.HasPrefix(arg, "-c="):
			if isModelConfigOverride(strings.TrimPrefix(arg, "-c=")) {
				return true
			}
		case strings.HasPrefix(arg, "--config="):
			if isModelConfigOverride(strings.TrimPrefix(arg, "--config=")) {
				return true
			}
		}
	}
	return false
}

func isModelConfigOverride(arg string) bool {
	key, _, ok := strings.Cut(arg, "=")
	return ok && strings.TrimSpace(key) == "model"
}

func teamSummary(team string) string {
	if team == "" {
		return "global (~/.hcom)"
	}
	return team
}

func shellCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellquote.Quote(arg))
	}
	return strings.Join(quoted, " ")
}

func hcomList(hcomDir string) []hcomEntry {
	cmd := exec.Command("hcom", "list", "--json")
	cmd.Env = append(os.Environ(), "HCOM_DIR="+hcomDir)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil
	}
	var entries []hcomEntry
	if json.Unmarshal(stdout.Bytes(), &entries) != nil {
		return nil
	}
	return entries
}

func parseWorkspaces(out []byte) []workspace {
	var envelope struct {
		Result struct {
			Workspaces []workspace `json:"workspaces"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &envelope) != nil {
		return nil
	}
	return envelope.Result.Workspaces
}

func workspaceExists(workspaces []workspace, id string) bool {
	for _, ws := range workspaces {
		if ws.WorkspaceID == id {
			return true
		}
	}
	return false
}

func spawnLabel(role, labelPrefix, short string) string {
	prefix := role
	if labelPrefix != "" {
		prefix = labelPrefix
	}
	return prefix + "-" + short
}

func compactMessage(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// parseWorktreeSource extracts the parent checkout path from `herdr worktree
// list --cwd … --json` (.result.source) — the directory `worktree create`
// must be pointed at, since herdr refuses to create from a linked worktree.
func parseWorktreeSource(out []byte) string {
	var envelope struct {
		Result struct {
			Source struct {
				SourceCheckoutPath string `json:"source_checkout_path"`
				RepoRoot           string `json:"repo_root"`
			} `json:"source"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &envelope) != nil {
		return ""
	}
	return firstNonEmpty(envelope.Result.Source.SourceCheckoutPath, envelope.Result.Source.RepoRoot)
}

// worktreeCreatePayload is the slice of `herdr worktree create --json` herder
// spawn consumes: the new workspace and checkout to spawn into, plus the seed
// tab/root-pane coordinates that feed the guarded seed-pane close.
type worktreeCreatePayload struct {
	WorkspaceID    string
	CheckoutPath   string
	TabID          string
	RootPaneID     string
	RootTerminalID string
}

func parseWorktreeCreate(out []byte) worktreeCreatePayload {
	var envelope struct {
		Result struct {
			Workspace struct {
				WorkspaceID string `json:"workspace_id"`
				Worktree    struct {
					CheckoutPath string `json:"checkout_path"`
				} `json:"worktree"`
			} `json:"workspace"`
			Tab struct {
				TabID string `json:"tab_id"`
			} `json:"tab"`
			RootPane struct {
				PaneID     string `json:"pane_id"`
				TerminalID string `json:"terminal_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &envelope) != nil {
		return worktreeCreatePayload{}
	}
	return worktreeCreatePayload{
		WorkspaceID:    envelope.Result.Workspace.WorkspaceID,
		CheckoutPath:   envelope.Result.Workspace.Worktree.CheckoutPath,
		TabID:          envelope.Result.Tab.TabID,
		RootPaneID:     envelope.Result.RootPane.PaneID,
		RootTerminalID: envelope.Result.RootPane.TerminalID,
	}
}

type agentStartPayload struct {
	Agent struct {
		PaneID      string `json:"pane_id"`
		WorkspaceID string `json:"workspace_id"`
		TabID       string `json:"tab_id"`
		TerminalID  string `json:"terminal_id"`
		CWD         string `json:"cwd"`
	} `json:"agent"`
	Type string `json:"type"`
}

func parseAgentStart(out []byte) (agentStartPayload, error) {
	var envelope struct {
		Result agentStartPayload `json:"result"`
	}
	err := json.Unmarshal(out, &envelope)
	return envelope.Result, err
}

func registryCapturedName(path, guid string) string {
	for i := 0; i < 6; i++ {
		if name := registryCapturedNameOnce(path, guid); name != "" {
			return name
		}
		sleepMS(700)
	}
	return ""
}

// registryCapturedNameOnce is a single-shot read of the sidecar's registry
// enrichment for guid — the earliest place the child's bus name appears.
func registryCapturedNameOnce(path, guid string) string {
	recs, err := registry.Load(path)
	if err != nil {
		return ""
	}
	for _, rec := range registry.LatestByGUID(recs) {
		if rec.GUID != nil && *rec.GUID == guid && rec.HcomName != "" {
			return rec.HcomName
		}
	}
	return ""
}

// childBoundBusOnce makes ONE attempt to learn the child's bus name using
// CHILD-SPECIFIC signals only: the sidecar's registry enrichment for THIS
// guid, or the hcom roster entry whose launch_context matches the frozen
// launch pane. The post-write capture loop's tag+cwd-unique fallback is
// deliberately NOT consulted here: during the pre-bind window a PRE-EXISTING
// same-tag+cwd agent is the only roster match, so that heuristic would bind
// the initial prompt to the OLD session — silent misdelivery (codex review
// P1). A stale match therefore never satisfies the prompt gate; the caller
// keeps waiting for the child itself, to bind_timeout if it never appears.
func childBoundBusOnce(registryPath, guid, hcomDir, launchPaneID string) string {
	if name := registryCapturedNameOnce(registryPath, guid); name != "" {
		return name
	}
	for _, entry := range hcomList(hcomDir) {
		if entry.LaunchContext.PaneID == launchPaneID {
			return entry.Name
		}
	}
	return ""
}

// grokBoundBusOnce makes one generation-fenced status request to the owning
// seat bridge. A successful response proves both that the bridge is live now
// and which bus it owns; hcom's derived roster status is never a Grok liveness
// signal.
func grokBoundBusOnce(stateDir, guid, sessionID string) string {
	if stateDir == "" || guid == "" || sessionID == "" {
		return ""
	}
	client, err := grokbridge.DialClientForSession(grokbridge.SocketPath(stateDir, guid), sessionID)
	if err != nil {
		return ""
	}
	resp, err := client.Call(grokbridge.Request{Op: "status"})
	if err != nil || resp.Status == nil || resp.Status.PID <= 0 {
		return ""
	}
	return strings.TrimSpace(resp.Status.Bus)
}

// resendCommand renders the exact, copy-pasteable recovery command for a prompt
// that bind-timed out (nothing went on the wire — a resend is safe; TASK-036).
// BOTH the label and the prompt are shell-quoted with the same printf %q-
// compatible quoting herder uses elsewhere: the label is built from
// --label-prefix verbatim and metachar prefixes are accepted (bash_metachar
// golden), so an unquoted label could split at ;, expand $, glob *, or die on an
// unmatched backtick when pasted — exactly when recovery matters. A multi-line
// brief (and any notify appendix already folded in) round-trips verbatim too.
func resendCommand(label, prompt string) string {
	return "herder send " + shellquote.Quote(label) + " " + shellquote.Quote(prompt)
}

// resendCommandFor returns the recovery command for the --json surface, but ONLY
// for the delivery results where a resend is the documented remedy and provably
// safe — bind_timeout and ready_match_timeout (nothing went on the wire). Every
// other result (delivered, queued, blocked_trust_modal, send_failed, the paste
// variants) has its own remedy and must NOT carry a resend_command, so the field
// is omitempty and left "" here. Mirrors the writeSummary human line exactly.
func resendCommandFor(deliveryResult, label, prompt string) string {
	if deliveryResult == "bind_timeout" || deliveryResult == "ready_match_timeout" {
		return resendCommand(label, prompt)
	}
	return ""
}

func envInt(name string, fallback int) int {
	if value := os.Getenv(name); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// resolveSpawnerBus returns the notify target's hcom bus name so a finished
// worker can report completion over the bus. It keys on a POSITIVE identity,
// most explicit first: a --notify-to that names a registry peer (guid/short/
// label, else that peer's live pane/terminal id, else a seated session's recorded
// hcom_name), a --notify-to that IS a literal bus name live on the bus the
// child will join (TASK-023 — bus names are first-class addresses, consistent
// with send's HERDER_BUS=hcom affordance), the spawner's own guid
// ($HERDER_GUID, captured as spawnedBy), and finally the spawner's own
// pane/terminal coordinates — which is what identifies an ENROLLED spawner
// (enroll records pane_id/terminal_id but the session has no $HERDER_GUID in
// its environment, so guid-only resolution misclassified it as bus-less).
// Returns ("", false) when nothing matches (bus-less → hard error at the
// caller) and ("", true) when a pane/terminal coordinate is AMBIGUOUS — a
// reused pane holds several seated sessions and bus liveness can't single one out.
// The caller treats that second signal as warn-and-skip-notify (best-effort,
// TASK-017), NOT a hard error: a stale-row-cluttered registry must not misroute
// a completion report the way `herder send` used to (TASK-035), and must not
// block the spawn either.
func resolveSpawnerBus(registryPath, notifyTo, spawnedBy, spawnerPane, spawnerTerm, childHcomDir string, warn io.Writer) (string, bool) {
	recs, err := registry.Load(registryPath)
	if err != nil {
		// No readable registry: registry-keyed steps can't match, but an
		// explicit --notify-to may still validate as a literal bus name below.
		recs = nil
	}
	if notifyTo != "" {
		if rec := registry.Resolve(recs, notifyTo); rec != nil && rec.HcomName != "" {
			return rec.HcomName, false
		}
		if name, ambiguous := busNameByPaneLive(recs, notifyTo, childHcomDir, warn); name != "" {
			return name, false
		} else if ambiguous {
			return "", true
		}
		if seatedBusName(recs, notifyTo) || liveOnBus(childHcomDir, notifyTo) {
			return notifyTo, false
		}
		// An EXPLICIT target that resolves nowhere is a hard error, not a
		// silent fallthrough to the spawner's own name — a typo'd --notify-to
		// must never quietly redirect completion reports.
		return "", false
	}
	if spawnedBy != "" && spawnedBy != "user" {
		if rec := registry.Resolve(recs, spawnedBy); rec != nil && rec.HcomName != "" {
			return rec.HcomName, false
		}
	}
	for _, key := range []string{spawnerPane, spawnerTerm} {
		if name, ambiguous := busNameByPaneLive(recs, key, childHcomDir, warn); name != "" {
			return name, false
		} else if ambiguous {
			return "", true
		}
	}
	return "", false
}

// busNameByPaneLive resolves a pane_id/terminal_id to a notify bus name,
// applying the same reused-pane discipline as `herder send` (TASK-035). A lone
// seated candidate resolves exactly as the old busNameByPane did — its
// hcom_name, unprobed (bus-less rows return "", not-yet-joined rows still
// resolve, so nothing that worked before breaks). Only when >1 seated session holds
// the coordinate is bus liveness on the CHILD's bus the tiebreaker: the single
// joined row wins. When 0 or >1 are live it returns ("", true) — ambiguous —
// after warning with the candidate list; the caller skips notify rather than
// route to a guessed session.
func busNameByPaneLive(recs []registry.Record, key, childHcomDir string, warn io.Writer) (string, bool) {
	candidates := registry.SeatedCandidatesByPaneOrTerminal(recs, key)
	switch len(candidates) {
	case 0:
		return "", false
	case 1:
		return candidates[0].HcomName, false
	default:
		chosen, live := registry.PickLiveCandidate(candidates, func(rec registry.Record) bool {
			return rec.HcomName != "" && liveOnBus(childHcomDir, rec.HcomName)
		})
		if chosen != nil {
			return chosen.HcomName, false
		}
		reason := "none joined on the child's bus"
		listRows := candidates
		if len(live) > 1 {
			reason = fmt.Sprintf("%d joined at once", len(live))
			listRows = live
		}
		if warn != nil {
			fmt.Fprintf(warn, "herder spawn: --notify pane %q is ambiguous (%d seated sessions, %s) — skipping notify rather than route a completion report to a guessed session. Candidates: %s\n", key, len(candidates), reason, candidateBusList(listRows))
		}
		return "", true
	}
}

// candidateBusList renders one-line guid=@bus pairs for an ambiguous-notify warning.
func candidateBusList(recs []registry.Record) string {
	parts := make([]string, 0, len(recs))
	for _, rec := range recs {
		name := "(no bus name)"
		if rec.HcomName != "" {
			name = "@" + rec.HcomName
		}
		guid := ""
		if rec.GUID != nil {
			guid = *rec.GUID
		}
		parts = append(parts, fmt.Sprintf("%s=%s", guid, name))
	}
	return strings.Join(parts, ", ")
}

// seatedBusName reports whether name is the recorded hcom_name of a seated
// registry row — --notify-to may address a peer directly by its bus name, not
// only by guid/label/pane coordinates. Non-seated sessions don't count: the bus
// recycles names, so a stale row must not vouch for a live address.
func seatedBusName(recs []registry.Record, name string) bool {
	for _, rec := range registry.LatestByGUID(recs) {
		if registry.IsSeated(rec) && rec.HcomName == name {
			return true
		}
	}
	return false
}

// liveOnBus reports whether name is currently joined on the bus at hcomDir —
// the bus the CHILD will send its notify on. This validates a literal
// --notify-to bus name that the registry doesn't know (e.g. an agent launched
// outside herder), while keeping cross-bus names (a global-bus peer for a
// --team child) a hard error: the child could never reach them anyway.
func liveOnBus(hcomDir, name string) bool {
	for _, entry := range hcomList(hcomDir) {
		if entry.Name == name {
			return true
		}
	}
	return false
}

func sleepMS(ms int) {
	if ms <= 0 || os.Getenv("MOCK_SPAWN_STATE") != "" {
		return
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}
