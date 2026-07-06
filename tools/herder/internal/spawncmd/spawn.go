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
	"sort"
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
	if opts.Workspace != "" && opts.FromPane != "" {
		die(stderr, "use --workspace or --from-pane, not both")
		return opts, 1
	}
	if opts.NewTab && opts.Tab != "" {
		die(stderr, "use --new-tab or --tab, not both")
		return opts, 1
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
	if opts.Workspace == "" && opts.FromPane == "" {
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
	if opts.Notify && opts.NotifyTo == "" {
		if paneID := os.Getenv("HERDR_PANE_ID"); paneID != "" {
			out, err := r.herdr.Output("pane", "get", paneID)
			if err == nil {
				pane, parseErr := herdrcli.ParsePaneGet(out)
				if parseErr == nil {
					opts.NotifyTo = pane.TerminalID
				}
			}
			if opts.NotifyTo == "" {
				opts.NotifyTo = paneID
				fmt.Fprintf(r.stderr, "herder spawn: could not resolve spawner terminal_id; ring target falls back to raw pane %s (drift-prone)\n", paneID)
			}
		} else {
			fmt.Fprintln(r.stderr, "herder spawn: --notify set but HERDR_PANE_ID is empty; no ring target injected")
		}
	}
	if opts.Notify && opts.Prompt != "" && opts.NotifyTo != "" {
		opts.Prompt += fmt.Sprintf(`

When your unit is finished and your run-log DONE/BLOCKED block is written, ring the orchestrator so it does not have to poll (the run-log block is the record — this is only a doorbell):
  "$HERDER_BIN" send %s 'Unit DONE — run-log updated'
Ring exactly ONCE and then stop, whatever it reports. The orchestrator is usually mid-turn when you ring, so the helper will say verify=queued (or even verify=not_delivered) — that is EXPECTED and is NOT a failure: your message is queued and the run-log is the real record. Do NOT resend on a queued/not_delivered result; resending just stacks duplicate messages in the orchestrator's queue.
Do NOT ring with raw 'herdr agent send' — it writes the text without submitting it, so the ring never lands. Use the command above (also available as "$HERDER_BIN" send "$HERDER_NOTIFY_TO").`, opts.NotifyTo)
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

	teamsRoot := os.Getenv("HERDER_TEAMS_ROOT")
	if teamsRoot == "" {
		teamsRoot = filepath.Join(os.Getenv("HOME"), ".hcom", "teams")
	}
	hcomDirEff := filepath.Join(os.Getenv("HOME"), ".hcom")
	if opts.Team != "" {
		hcomDirEff = filepath.Join(teamsRoot, opts.Team)
		_ = os.MkdirAll(hcomDirEff, 0o755)
	}

	childCWD := opts.CWD
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

	rootPaneID, rootTerm := "", ""
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

	notifyExport := ""
	if opts.NotifyTo != "" {
		notifyExport = " HERDER_NOTIFY_TO=" + shellquote.Quote(opts.NotifyTo)
	}
	spawnedBy := os.Getenv("HERDER_GUID")
	if spawnedBy == "" {
		spawnedBy = "user"
	}
	hcomEnv := ""
	if isHcomAgent {
		hcomEnv = " HCOM_DIR=" + shellquote.Quote(hcomDirEff)
	}
	argv := []string{}
	if opts.LoginShell {
		innerCmd := shellCommand(launchTokens)
		inner := fmt.Sprintf("export HERDER_GUID=%s HERDER_ROLE=%s HERDER_LABEL=%s HERDER_SPAWNED_BY=%s HERDER_BIN=%s%s%s; exec %s",
			shellquote.Quote(guid), shellquote.Quote(opts.Role), shellquote.Quote(label), shellquote.Quote(spawnedBy), shellquote.Quote(herderBin), notifyExport, hcomEnv, innerCmd)
		argv = []string{opts.LoginShellBin, "-lic", inner}
	} else {
		argv = []string{"env", "HERDER_GUID=" + guid, "HERDER_ROLE=" + opts.Role, "HERDER_LABEL=" + label, "HERDER_SPAWNED_BY=" + spawnedBy, "HERDER_BIN=" + herderBin}
		if opts.NotifyTo != "" {
			argv = append(argv, "HERDER_NOTIFY_TO="+opts.NotifyTo)
		}
		if isHcomAgent {
			argv = append(argv, "HCOM_DIR="+hcomDirEff)
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
	if opts.CWD != "" {
		startArgs = append(startArgs, "--cwd", opts.CWD)
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

	rootClosed := false
	if opts.NewTab && rootPaneID != "" && rootTerm != "" {
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
		out, rc := runSend(herderBin, paneID, wirePayload)
		if len(out) > 0 {
			deliveryResult = parseVerify(out)
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
		Provenance:           registry.BuildProvenance("spawn", opts.Role, resolvedCWD, wsID),
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
						sort.Slice(matches, func(i, j int) bool { return matches[i].CreatedAt < matches[j].CreatedAt })
						if len(matches) > 0 {
							record.HcomName = matches[len(matches)-1].Name
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

	r.writeSummary(record, isHcomAgent, rootClosed, permInjected, hcomCapture, briefFile, promptSent, deliveryResult, readyReason, trustBlocked)
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
		}
		b, _ := json.Marshal(outRecord)
		fmt.Fprintln(r.stdout, string(b))
	}
	return 0
}

func (r *runner) awaitReady(paneID *string) (reason string, trustBlocked bool, modalCleared bool) {
	prev := ""
	waited := 0
	status := ""
	for waited < r.opts.WaitTimeoutMS {
		text := r.paneText(*paneID)
		status = r.paneStatus(*paneID)
		if trustModalRE.MatchString(text) {
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
	return "timeout(status=" + status + suffix + ")", false, modalCleared
}

func (r *runner) paneText(paneID string) string {
	out, err := r.herdr.Output("agent", "read", paneID, "--source", "recent-unwrapped", "--lines", "40")
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

func (r *runner) writeSummary(record spawnRecord, isHcomAgent, rootClosed bool, permInjected, hcomCapture, briefFile string, promptSent bool, deliveryResult, readyReason string, trustBlocked bool) {
	fmt.Fprintf(r.stderr, "spawned %s (%s) in pane %s (workspace %s)\n", record.Label, record.Agent, record.PaneID, record.WorkspaceID)
	fmt.Fprintf(r.stderr, "  guid:   %s\n", record.GUID)
	fmt.Fprintf(r.stderr, "  cwd:    %s\n", record.CWD)
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
	if r.opts.Notify && r.opts.NotifyTo != "" {
		fmt.Fprintf(r.stderr, "  notify: rings %s on done (HERDER_BIN + HERDER_NOTIFY_TO injected)\n", r.opts.NotifyTo)
	}
	if isHcomAgent {
		if hcomCapture == "captured" {
			fmt.Fprintf(r.stderr, "  bus:    %s @%s  (team: %s)\n", firstNonEmpty(record.Team, "global"), record.HcomName, teamSummary(record.Team))
			fmt.Fprintf(r.stderr, "          HCOM_DIR=%s\n", record.HcomDir)
		} else {
			fmt.Fprintf(r.stderr, "  bus:    %s — name NOT captured (%s); reachable via herdr keystrokes only\n", firstNonEmpty(record.Team, "global"), hcomCapture)
			fmt.Fprintf(r.stderr, "          HCOM_DIR=%s\n", record.HcomDir)
		}
	} else {
		fmt.Fprintf(r.stderr, "  bus:    n/a (%s is not an hcom agent; herdr keystroke transport)\n", record.Agent)
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
		"               [--tab ID | --new-tab] [--cwd PATH] [--safe] [--notify | --notify-to PANE]",
		"               [--extra-arg ARG]... [--no-focus] [--json]",
		"",
		"Options:",
		"  --role R          agent role; becomes the hcom --tag and label prefix (required)",
		"  --agent A         tool to run: claude, codex, gemini, bash, ... (required)",
		"  --prompt TEXT     initial prompt (or --prompt-file F), delivered verified once ready",
		"  --team NAME       join the bus at $HERDER_TEAMS_ROOT/<NAME> (default: global ~/.hcom)",
		"  --split D         pane split: right (default) or down",
		"  --workspace ID    place in this workspace; --from-pane PANE_ID copies another pane's",
		"  --tab ID          add to an existing tab; --new-tab gives the agent its own fresh tab",
		"  --cwd PATH        working directory for the agent (default: current)",
		"  --safe            keep the agent's default ask-mode (default: autonomous/skip-permissions)",
		"  --notify          worker rings the spawner on done; --notify-to PANE overrides the target",
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
		"  and are reachable over herdr keystrokes only. The assigned bus name, team, and hcom",
		"  coordinates are captured into the registry so send/wait/cull can resolve this agent.",
		"",
		"  Initial-prompt delivery is verified. If it can't be confirmed the summary reports",
		"  \"prompt: NOT confirmed\" — read the pane (`herder wait <guid> --read`) before assuming",
		"  it landed; a blind resend double-submits. For codex, a long or multi-line brief is",
		"  staged to $HERDER_STATE_DIR/briefs/<guid>.md and only a one-line pointer is sent to",
		"  the pane (dodges codex paste-blob / plan-overlay pathologies).",
		"",
		"  --notify is the doorbell: a finished worker rings the spawner instead of the spawner",
		"  polling wait in a loop. The run-log stays the record; the ring is only a signal.",
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

func parseVerify(out []byte) string {
	var record struct {
		Verify string `json:"verify"`
	}
	if json.Unmarshal(out, &record) != nil || record.Verify == "" {
		return "unknown"
	}
	return record.Verify
}

func runSend(herderBin, paneID, payload string) ([]byte, int) {
	cmd := exec.Command(herderBin, "send", paneID, payload, "--json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), 0
	}
	var exitErr *exec.ExitError
	if ok := errorAs(err, &exitErr); ok {
		return stdout.Bytes(), exitErr.ExitCode()
	}
	return stdout.Bytes(), 1
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

func sleepMS(ms int) {
	if ms <= 0 || os.Getenv("MOCK_SPAWN_STATE") != "" {
		return
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func errorAs(err error, target any) bool {
	switch t := target.(type) {
	case **exec.ExitError:
		if e, ok := err.(*exec.ExitError); ok {
			*t = e
			return true
		}
	}
	return false
}
