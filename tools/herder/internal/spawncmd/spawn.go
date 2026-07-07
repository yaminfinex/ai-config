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

	"ai-config/tools/herder/internal/herderpaths"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/launchcmd"
	"ai-config/tools/herder/internal/registry"
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
	JSONOutput    bool
	WaitTimeoutMS int
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
	PermInjected         string              `json:"perm_injected"`
	NewTab               bool                `json:"new_tab"`
	RootPaneClosed       bool                `json:"root_pane_closed"`
	HcomCapture          string              `json:"hcom_capture"`
	BriefFile            string              `json:"brief_file,omitempty"`
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
	opts   options
	stdout io.Writer
	stderr io.Writer
	herdr  *herdrcli.Client
	paths  herderpaths.Paths
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	opts := options{
		Split:         "right",
		FocusFlag:     "--no-focus",
		WaitTimeoutMS: envInt("HERDER_SPAWN_WAIT_MS", 15000),
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
	if opts.NewTab && opts.Tab != "" {
		die(stderr, "use --new-tab or --tab, not both")
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
		opts.NotifyBusName = resolveSpawnerBus(registryPath, opts.NotifyTo, spawnedBy, spawnerPaneID, spawnerTermID, hcomDirEff)
		if opts.NotifyBusName == "" {
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
	label := opts.LabelPrefix + opts.Role + "-" + short

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
	// seed tab + root shell pane, so the payload's coordinates feed the exact
	// machinery --new-tab already uses: agent start into that tab, then the
	// guarded seed-pane close. Everything herdr-side (branch, checkout path,
	// workspace) stays herdr-owned — herder only wraps it.
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

	// Checkout-scoped env hygiene: a child spawned --cwd into a DIFFERENT
	// ai-config checkout (typically a worktree) would otherwise inherit the
	// spawner's AI_CONFIG_ROOT and HERDER_BIN pointing at the spawning tree —
	// bin/herder and lib/common.sh let the env var win over their own location,
	// so the child's builds and suites silently exercise the wrong tree. When
	// the child's cwd resolves to another checkout, re-point both at it. The
	// spawn-time launch itself (launchTokens) stays on the SPAWNER's bin/herder:
	// it is the proven-buildable tree, and a mid-work child tree must not be
	// able to brick its own boot. Outside any checkout the inherited values are
	// left alone — there is no wrong tree to protect against, and the spawner's
	// herder is the only one the child can call.
	childEnvBin := herderBin
	childEnvRoot := ""
	if root, bin, ok := checkoutForDir(childCWD); ok && root != r.paths.RepoRoot {
		childEnvBin = bin
		childEnvRoot = root
	}

	if opts.NewTab {
		tabArgs := []string{"tab", "create", "--no-focus", "--label", label}
		if opts.Workspace != "" {
			tabArgs = append(tabArgs, "--workspace", opts.Workspace)
		}
		if opts.CWD != "" {
			tabArgs = append(tabArgs, "--cwd", opts.CWD)
		}
		out, rc, _ := r.herdr.Combined(tabArgs...)
		if rc != 0 {
			fmt.Fprintf(r.stderr, "herdr tab create failed:\n%s\n", strings.TrimRight(string(out), "\n"))
			return rc
		}
		tabID, rootID, rootTerminal := parseTabCreate(out)
		opts.Tab, rootPaneID, rootTerm = tabID, rootID, rootTerminal
		if opts.Tab == "" || rootPaneID == "" {
			fmt.Fprintf(r.stderr, "unexpected tab create payload: %s\n", strings.TrimRight(string(out), "\n"))
			return 1
		}
	}

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
	rootExport := ""
	if childEnvRoot != "" {
		rootExport = " AI_CONFIG_ROOT=" + shellquote.Quote(childEnvRoot)
	}
	argv := []string{}
	if opts.LoginShell {
		innerCmd := shellCommand(launchTokens)
		inner := fmt.Sprintf("%sexport HERDER_GUID=%s HERDER_ROLE=%s HERDER_LABEL=%s HERDER_SPAWNED_BY=%s HERDER_BIN=%s%s%s; exec %s",
			misePathFix, shellquote.Quote(guid), shellquote.Quote(opts.Role), shellquote.Quote(label), shellquote.Quote(spawnedBy), shellquote.Quote(childEnvBin), rootExport, hcomEnv, innerCmd)
		argv = []string{opts.LoginShellBin, "-lic", inner}
	} else {
		// The env form has no shell, so it gets the checkout re-point but not
		// the mise shims PATH fix (that one needs runtime expansion).
		argv = []string{"env", "HERDER_GUID=" + guid, "HERDER_ROLE=" + opts.Role, "HERDER_LABEL=" + label, "HERDER_SPAWNED_BY=" + spawnedBy, "HERDER_BIN=" + childEnvBin}
		if childEnvRoot != "" {
			argv = append(argv, "AI_CONFIG_ROOT="+childEnvRoot)
		}
		if isHcomAgent {
			argv = append(argv, "HCOM_DIR="+hcomDirEff, "PATH="+r.paths.ShimsDir+string(os.PathListSeparator)+os.Getenv("PATH"))
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
	resolvedCWDPhys := resolvedCWD
	if phys, err := filepath.EvalSymlinks(resolvedCWD); err == nil {
		resolvedCWDPhys = phys
	}
	launchPaneID := paneID

	// Seed-pane close: --new-tab's tab create and --worktree's workspace create
	// both leave a root shell pane behind; the identity-guarded close applies
	// to whichever path populated rootPaneID/rootTerm.
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

	readyReason := ""
	trustBlocked := false
	modalCleared := false
	if opts.NoReadyWait {
		readyReason = "ready-wait skipped (--no-ready-wait)"
	} else {
		readyReason, trustBlocked, modalCleared = r.awaitReady(&paneID)
		sleepMS(opts.SettleMS)
		_ = modalCleared
	}

	promptSent := false
	deliveryResult := "not_attempted"
	wirePayload := opts.Prompt
	briefFile := ""
	if opts.Prompt != "" && opts.Agent == "codex" && (strings.Contains(opts.Prompt, "\n") || len(opts.Prompt) > 800) {
		briefDir := filepath.Join(stateDir, "briefs")
		if err := os.MkdirAll(briefDir, 0o755); err != nil {
			die(r.stderr, err.Error())
			return 1
		}
		briefFile = filepath.Join(briefDir, guid+".md")
		if err := os.WriteFile(briefFile, []byte(opts.Prompt+"\n"), 0o644); err != nil {
			die(r.stderr, err.Error())
			return 1
		}
		wirePayload = "Read " + briefFile + " in full (it is your complete brief), then plan before writing code."
	}

	if opts.Prompt != "" && trustBlocked {
		deliveryResult = "blocked_trust_modal"
	} else if opts.Prompt != "" {
		verify, rc := (&bootPaster{Client: r.herdr}).paste(paneID, wirePayload)
		if verify != "" {
			deliveryResult = verify
		}
		if rc == 0 {
			promptSent = true
		}
	}

	hcomDirRec, hcomTagRec := "", ""
	if isHcomAgent {
		hcomDirRec = hcomDirEff
		hcomTagRec = opts.Role
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
		HcomName:             "",
		HcomTag:              hcomTagRec,
		Status:               "active",
		Provenance:           registry.BuildProvenance("spawn", spawnedBy, opts.Role, resolvedCWD, wsID),
	}
	registryLine, _ := json.Marshal(record)
	if err := appendLine(registryPath, registryLine); err != nil {
		die(r.stderr, err.Error())
		return 1
	}

	hcomCapture := "not_hcom_agent"
	if isHcomAgent {
		hcomCapture = "not_found"
		if name := registryCapturedName(registryPath, guid); name != "" {
			record.HcomName = name
			hcomCapture = "captured"
		} else {
			for i := 0; i < 6; i++ {
				entries := hcomList(hcomDirEff)
				if len(entries) > 0 {
					for _, entry := range entries {
						if entry.LaunchContext.PaneID == launchPaneID {
							record.HcomName = entry.Name
							break
						}
					}
					if record.HcomName == "" {
						var matches []hcomEntry
						for _, entry := range entries {
							if entry.Tag == opts.Role && (entry.Directory == resolvedCWD || entry.Directory == resolvedCWDPhys) {
								matches = append(matches, entry)
							}
						}
						// Only a UNIQUE tag+cwd match is trustworthy without a
						// positive pane correlate. Two or more live entries
						// sharing tag+cwd cannot be told apart; picking the newest
						// (latest-wins) silently captures an unrelated agent's
						// identity — the wrong-guid enrichment bug. Refuse to
						// guess: record the capture as ambiguous and stop.
						if len(matches) == 1 {
							record.HcomName = matches[0].Name
						} else if len(matches) > 1 {
							hcomCapture = "ambiguous"
							break
						}
					}
					if record.HcomName != "" {
						hcomCapture = "captured"
						updated, _ := json.Marshal(record)
						if err := appendLine(registryPath, updated); err != nil {
							die(r.stderr, err.Error())
							return 1
						}
						break
					}
				}
				sleepMS(700)
			}
		}
	}

	r.writeSummary(record, wtInfo, isHcomAgent, rootClosed, permInjected, hcomCapture, briefFile, promptSent, deliveryResult, readyReason, trustBlocked)
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
			PermInjected:         permInjected,
			NewTab:               opts.NewTab,
			RootPaneClosed:       rootClosed,
			HcomCapture:          hcomCapture,
			BriefFile:            briefFile,
			Worktree:             wtInfo,
		}
		b, _ := json.Marshal(outRecord)
		fmt.Fprintln(r.stdout, string(b))
	}
	spawnCompleted = true
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

func (r *runner) writeSummary(record spawnRecord, wtInfo *worktreeInfo, isHcomAgent, rootClosed bool, permInjected, hcomCapture, briefFile string, promptSent bool, deliveryResult, readyReason string, trustBlocked bool) {
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
	}
	if r.opts.NewTab {
		if rootClosed {
			fmt.Fprintf(r.stderr, "  tab:    %s (new, root shell closed; agent is sole pane)\n", r.opts.Tab)
		} else {
			fmt.Fprintf(r.stderr, "  tab:    %s (new; WARNING: root shell NOT closed — spare pane may remain)\n", r.opts.Tab)
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
	if briefFile != "" {
		fmt.Fprintf(r.stderr, "  brief:  staged to %s (codex; sent a one-line pointer to dodge paste-blob/plan-overlay)\n", briefFile)
	}
	if r.opts.Prompt != "" {
		if promptSent {
			fmt.Fprintf(r.stderr, "  prompt: sent (%d chars), ready: %s, verify: %s\n", len(r.opts.Prompt), readyReason, deliveryResult)
		} else if deliveryResult == "blocked_trust_modal" {
			fmt.Fprintln(r.stderr, "  prompt: NOT sent — a directory-trust modal is open and --safe forbids auto-accepting it.")
			fmt.Fprintf(r.stderr, "          Accept it in the pane (focus + Enter), then: herder send %s \"<prompt>\"\n", record.Label)
		} else {
			fmt.Fprintf(r.stderr, "  prompt: NOT confirmed (verify: %s, ready: %s) — delivery unverified\n", deliveryResult, readyReason)
			fmt.Fprintf(r.stderr, "          read the pane first: herder wait %s --read; do NOT blind-resend (double-submits)\n", record.Label)
		}
	} else if trustBlocked {
		fmt.Fprintln(r.stderr, "  note:   directory-trust modal is open (--safe); accept it in the pane to use the agent")
	}
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
		"               [--notify | --notify-to TARGET] [--extra-arg ARG]... [--no-focus] [--json]",
		"",
		"Options:",
		"  --role R          agent role; becomes the hcom --tag and label prefix (required)",
		"  --agent A         tool to run: claude, codex, gemini, bash, ... (required)",
		"  --prompt TEXT     initial prompt (or --prompt-file F), delivered verified once ready",
		"  --team NAME       join the bus at $HERDER_TEAMS_ROOT/<NAME> (default: global ~/.hcom)",
		"  --split D         pane split: right (default) or down",
		"  --workspace ID    place in this workspace; --from-pane PANE_ID copies another pane's",
		"  --tab ID          add to an existing tab; --new-tab gives the agent its own fresh tab",
		"  --worktree BRANCH create a fresh git worktree on BRANCH (via `herdr worktree create`) and",
		"                    spawn into its workspace in one step; --base REF picks the start point",
		"  --cwd PATH        working directory for the agent (default: current)",
		"  --safe            keep the agent's default ask-mode (default: autonomous/skip-permissions)",
		"  --notify          worker reports to the spawner over the hcom bus on done (requires --prompt",
		"                    and a bus-bound spawner); --notify-to TARGET resolves the bus name from",
		"                    another registry row (guid, label, terminal_id, pane_id, or recorded hcom",
		"                    name), else accepts TARGET as a literal bus name if live on the child's bus",
		"  --extra-arg ARG   pass ARG through to the agent (repeatable)",
		"  --json            print the registry record as JSON on stdout",
		"",
		"Advanced:",
		"  --label-prefix STR    override the label prefix (default: the role)",
		"  --no-login-shell      run the agent without a login+interactive shell wrapper",
		"  --wait-timeout-ms MS  max wait for the agent to become ready before sending the prompt",
		"  --ready-match STR     treat the pane as ready when its screen matches STR",
		"  --no-ready-wait       send the prompt immediately, skipping the readiness wait",
		"",
		"Behavior:",
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
		"  Initial-prompt delivery is typed into the freshly booted pane and verified — the one",
		"  deliberate keystroke path that remains (the agent has no bus binding yet at boot);",
		"  every later message goes through `herder send` over the hcom bus.",
		"  If it can't be confirmed the summary reports",
		"  \"prompt: NOT confirmed\" — read the pane (`herder wait <guid> --read`) before assuming",
		"  it landed; a blind resend double-submits. For codex, a long or multi-line brief is",
		"  staged to $HERDER_STATE_DIR/briefs/<guid>.md and only a one-line pointer is sent to",
		"  the pane (dodges codex paste-blob / plan-overlay pathologies).",
		"",
		"  --worktree wraps `herdr worktree create` (worktree/workspace lifecycle stays herdr-owned):",
		"  the source repo resolves from the spawner's cwd (works from inside a linked worktree),",
		"  the created workspace's seed shell pane is closed under the same identity guard as",
		"  --new-tab, and the summary + --json surface the worktree coordinates (workspace_id,",
		"  checkout path, branch) for later lifecycle management. If the worktree is created but the",
		"  spawn then fails, NOTHING is auto-removed — the failure report names the workspace and the",
		"  exact `herdr worktree remove` command. The workspace label stays herdr's branch-derived",
		"  default (it names the TREE); the agent label stays role-short (it names the AGENT).",
		"",
		"  --notify is the doorbell: a finished worker reports to the spawner over the hcom bus so",
		"  it needn't poll wait in a loop. The spawner's bus name resolves from the registry by its",
		"  guid AND by its pane/terminal coordinates, so enrolled sessions (no $HERDER_GUID in their",
		"  environment) route bus-native too. --notify-to may also name the target directly by its",
		"  bus name: an active registry row's hcom_name matches, and an unregistered name is accepted",
		"  if live on the bus the child will join (team-scoped — cross-bus names still refuse).",
		"  A spawner that resolves to NO bus name is a hard error (the keystroke ring was removed).",
		"  It is only a signal — send it once and stop.",
		"",
		"  Child environment: the SPAWNING checkout's tools/herder/shims dir is prepended to the",
		"  child's PATH (hcom agents), so spawning from a worktree injects THAT worktree's shims —",
		"  by design; sibling shims recognize each other by marker and never loop. The login-shell",
		"  form also pins mise's shims dir to the front of PATH, so mise-managed toolchains beat",
		"  system ones even though rc-file mise activation is prompt-hook driven and inert in a",
		"  spawned pane. When --cwd lands the child in a DIFFERENT ai-config checkout (a worktree),",
		"  AI_CONFIG_ROOT and HERDER_BIN are re-pointed at that checkout so the child builds and",
		"  tests its own tree; the spawn-time launch itself still rides the spawner's bin/herder.",
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
	default:
		return ""
	}
}

func hasExplicitPermFlag(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--dangerously-skip-permissions", "--permission-mode", "--permission-prompt-tool",
			"--dangerously-bypass-approvals-and-sandbox", "--full-auto", "-a", "--ask-for-approval", "-s", "--sandbox":
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

func parseTabCreate(out []byte) (tabID, rootPaneID, rootTerm string) {
	var envelope struct {
		Result struct {
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
		return "", "", ""
	}
	return envelope.Result.Tab.TabID, envelope.Result.RootPane.PaneID, envelope.Result.RootPane.TerminalID
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

func appendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return err
	}
	_, err = f.Write([]byte("\n"))
	return err
}

func registryCapturedName(path, guid string) string {
	for i := 0; i < 6; i++ {
		recs, err := registry.Load(path)
		if err == nil {
			for _, rec := range registry.LatestByGUID(recs) {
				if rec.GUID != nil && *rec.GUID == guid && rec.HcomName != "" {
					return rec.HcomName
				}
			}
		}
		sleepMS(700)
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
// label, else that peer's live pane/terminal id, else an active row's recorded
// hcom_name), a --notify-to that IS a literal bus name live on the bus the
// child will join (TASK-023 — bus names are first-class addresses, consistent
// with send's HERDER_BUS=hcom affordance), the spawner's own guid
// ($HERDER_GUID, captured as spawnedBy), and finally the spawner's own
// pane/terminal coordinates — which is what identifies an ENROLLED spawner
// (enroll records pane_id/terminal_id but the session has no $HERDER_GUID in
// its environment, so guid-only resolution misclassified it as bus-less).
// Returns "" when nothing matches (bus-less → hard error at the caller).
func resolveSpawnerBus(registryPath, notifyTo, spawnedBy, spawnerPane, spawnerTerm, childHcomDir string) string {
	recs, err := registry.Load(registryPath)
	if err != nil {
		// No readable registry: registry-keyed steps can't match, but an
		// explicit --notify-to may still validate as a literal bus name below.
		recs = nil
	}
	if notifyTo != "" {
		if rec := registry.Resolve(recs, notifyTo); rec != nil && rec.HcomName != "" {
			return rec.HcomName
		}
		if name := busNameByPane(recs, notifyTo); name != "" {
			return name
		}
		if activeBusName(recs, notifyTo) || liveOnBus(childHcomDir, notifyTo) {
			return notifyTo
		}
		// An EXPLICIT target that resolves nowhere is a hard error, not a
		// silent fallthrough to the spawner's own name — a typo'd --notify-to
		// must never quietly redirect completion reports.
		return ""
	}
	if spawnedBy != "" && spawnedBy != "user" {
		if rec := registry.Resolve(recs, spawnedBy); rec != nil && rec.HcomName != "" {
			return rec.HcomName
		}
	}
	for _, key := range []string{spawnerPane, spawnerTerm} {
		if name := busNameByPane(recs, key); name != "" {
			return name
		}
	}
	return ""
}

// busNameByPane resolves a live pane_id/terminal_id to the bus name of the
// latest ACTIVE row holding it. Pane coordinates are positional (herdr reuses
// them across sessions), so unlike guid/label resolution this refuses closed
// rows; ties resolve last in guid order, matching the registry's jq semantics.
func busNameByPane(recs []registry.Record, key string) string {
	if key == "" {
		return ""
	}
	name := ""
	for _, rec := range registry.LatestByGUID(recs) {
		if rec.Status == "active" && rec.HcomName != "" && (rec.PaneID == key || rec.TerminalID == key) {
			name = rec.HcomName
		}
	}
	return name
}

// activeBusName reports whether name is the recorded hcom_name of an ACTIVE
// registry row — --notify-to may address a peer directly by its bus name, not
// only by guid/label/pane coordinates. Closed rows don't count: the bus
// recycles names, so a stale row must not vouch for a live address.
func activeBusName(recs []registry.Record, name string) bool {
	for _, rec := range registry.LatestByGUID(recs) {
		if rec.Status == "active" && rec.HcomName == name {
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

// checkoutForDir walks dir upward to the nearest ai-config checkout, using the
// same criteria as herderpaths (an executable bin/herder plus a
// tools/herder/shims dir). It answers "which tree does the CHILD's cwd belong
// to", which herderpaths.Resolve cannot: that resolves the SPAWNER's tree from
// $AI_CONFIG_ROOT/getwd — exactly the inherited value being corrected here.
func checkoutForDir(dir string) (root, binHerder string, ok bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", false
	}
	for {
		bin := filepath.Join(abs, "bin", "herder")
		shims := filepath.Join(abs, "tools", "herder", "shims")
		if binSt, err := os.Stat(bin); err == nil && !binSt.IsDir() && binSt.Mode()&0o111 != 0 {
			if shimsSt, err := os.Stat(shims); err == nil && shimsSt.IsDir() {
				return abs, bin, true
			}
		}
		next := filepath.Dir(abs)
		if next == abs {
			return "", "", false
		}
		abs = next
	}
}

func sleepMS(ms int) {
	if ms <= 0 || os.Getenv("MOCK_SPAWN_STATE") != "" {
		return
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

