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

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/launchcmd"
	"ai-config/tools/herder/internal/registry"
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
	opts      options
	stdout    io.Writer
	stderr    io.Writer
	herdr     *herdrcli.Client
	scriptDir string
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
		switch arg {
		case "--role":
			if i+1 >= len(args) {
				die(stderr, "unknown arg: "+arg)
				return opts, 1
			}
			opts.Role = args[i+1]
			i += 2
		case "--agent":
			if i+1 >= len(args) {
				die(stderr, "unknown arg: "+arg)
				return opts, 1
			}
			opts.Agent = args[i+1]
			i += 2
		case "--prompt":
			if i+1 >= len(args) {
				die(stderr, "unknown arg: "+arg)
				return opts, 1
			}
			opts.Prompt = args[i+1]
			i += 2
		case "--prompt-file":
			if i+1 >= len(args) {
				die(stderr, "unknown arg: "+arg)
				return opts, 1
			}
			opts.PromptFile = args[i+1]
			i += 2
		case "--split":
			opts.Split = args[i+1]
			i += 2
		case "--workspace":
			opts.Workspace = args[i+1]
			i += 2
		case "--from-pane":
			opts.FromPane = args[i+1]
			i += 2
		case "--tab":
			opts.Tab = args[i+1]
			i += 2
		case "--new-tab":
			opts.NewTab = true
			i++
		case "--cwd":
			opts.CWD = args[i+1]
			i += 2
		case "--focus":
			opts.FocusFlag = "--focus"
			i++
		case "--no-focus":
			opts.FocusFlag = "--no-focus"
			i++
		case "--label-prefix":
			opts.LabelPrefix = args[i+1]
			i += 2
		case "--extra-arg":
			opts.ExtraArgs = append(opts.ExtraArgs, args[i+1])
			i += 2
		case "--json":
			opts.JSONOutput = true
			i++
		case "--wait-timeout-ms":
			opts.WaitTimeoutMS, _ = strconv.Atoi(args[i+1])
			i += 2
		case "--ready-match":
			opts.ReadyMatch = args[i+1]
			i += 2
		case "--no-ready-wait":
			opts.NoReadyWait = true
			i++
		case "--no-login-shell":
			opts.LoginShell = false
			i++
		case "--login-shell":
			opts.LoginShellBin = args[i+1]
			i += 2
		case "--safe":
			opts.Safe = true
			i++
		case "--team":
			opts.Team = args[i+1]
			i += 2
		case "--notify":
			opts.Notify = true
			i++
		case "--notify-to":
			opts.Notify = true
			opts.NotifyTo = args[i+1]
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
	r.scriptDir, err = findScriptDir()
	if err != nil {
		die(r.stderr, err.Error())
		return 1
	}

	opts := &r.opts
	var wsListOut []byte
	var workspaces []workspace

	if opts.FromPane != "" {
		out, _, _ := r.herdr.Combined("pane", "get", opts.FromPane)
		pane, parseErr := herdrcli.ParsePaneGet(out)
		if parseErr == nil {
			opts.Workspace = pane.WorkspaceID
		}
		if opts.Workspace == "" {
			fmt.Fprintf(r.stderr, "herder-spawn: --from-pane %s: pane not found (herdr pane get returned: %s)\n", opts.FromPane, strings.TrimRight(string(out), "\n"))
			return 1
		}
	}

	if opts.Workspace != "" {
		wsListOut, _, _ = r.herdr.Combined("workspace", "list")
		workspaces = parseWorkspaces(wsListOut)
		if !workspaceExists(workspaces, opts.Workspace) {
			fmt.Fprintf(r.stderr, "herder-spawn: --workspace %s not found in live workspace list.\n", opts.Workspace)
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

	sendAbs := filepath.Join(r.scriptDir, "herder-send")
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
				fmt.Fprintf(r.stderr, "herder-spawn: could not resolve spawner terminal_id; ring target falls back to raw pane %s (drift-prone)\n", paneID)
			}
		} else {
			fmt.Fprintln(r.stderr, "herder-spawn: --notify set but HERDR_PANE_ID is empty; no ring target injected")
		}
	}
	if opts.Notify && opts.Prompt != "" && opts.NotifyTo != "" {
		opts.Prompt += fmt.Sprintf(`

When your unit is finished and your run-log DONE/BLOCKED block is written, ring the orchestrator so it does not have to poll (the run-log block is the record — this is only a doorbell):
  %s %s 'Unit DONE — run-log updated'
Ring exactly ONCE and then stop, whatever it reports. The orchestrator is usually mid-turn when you ring, so the helper will say verify=queued (or even verify=not_delivered) — that is EXPECTED and is NOT a failure: your message is queued and the run-log is the real record. Do NOT resend on a queued/not_delivered result; resending just stacks duplicate messages in the orchestrator's queue.
Do NOT ring with raw 'herdr agent send' — it writes the text without submitting it, so the ring never lands. Use the command above (also available as "$HERDER_SEND" "$HERDER_NOTIFY_TO").`, sendAbs, opts.NotifyTo)
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
	hcomLaunch := filepath.Join(r.scriptDir, "hcom-launch")
	if isHcomAgent {
		if _, err := exec.LookPath("hcom"); err != nil {
			die(r.stderr, "hcom is required to spawn '"+opts.Agent+"' (launch-through-hcom); install hcom or spawn --agent bash")
			return 1
		}
		if st, err := os.Stat(hcomLaunch); err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
			die(r.stderr, "hcom-launch wrapper missing/not executable: "+hcomLaunch)
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
		launchTokens = append(launchTokens, hcomLaunch, opts.Agent, "--tag", opts.Role)
		launchTokens = append(launchTokens, opts.ExtraArgs...)
	} else {
		launchTokens = append(launchTokens, opts.Agent)
		launchTokens = append(launchTokens, opts.ExtraArgs...)
	}

	notifyExport := ""
	if opts.NotifyTo != "" {
		notifyExport = " HERDER_NOTIFY_TO=" + shellQuote(opts.NotifyTo)
	}
	spawnedBy := os.Getenv("HERDER_GUID")
	if spawnedBy == "" {
		spawnedBy = "user"
	}
	hcomEnv := ""
	if isHcomAgent {
		hcomEnv = " HCOM_DIR=" + shellQuote(hcomDirEff)
	}
	argv := []string{}
	if opts.LoginShell {
		var innerCmd strings.Builder
		for _, arg := range launchTokens {
			innerCmd.WriteString(shellQuote(arg))
			innerCmd.WriteByte(' ')
		}
		inner := fmt.Sprintf("export HERDER_GUID=%s HERDER_ROLE=%s HERDER_LABEL=%s HERDER_SPAWNED_BY=%s HERDER_SEND=%s%s%s; exec %s",
			shellQuote(guid), shellQuote(opts.Role), shellQuote(label), shellQuote(spawnedBy), shellQuote(sendAbs), notifyExport, hcomEnv, innerCmd.String())
		argv = []string{opts.LoginShellBin, "-lic", inner}
	} else {
		argv = []string{"env", "HERDER_GUID=" + guid, "HERDER_ROLE=" + opts.Role, "HERDER_LABEL=" + label, "HERDER_SPAWNED_BY=" + spawnedBy, "HERDER_SEND=" + sendAbs}
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
			fmt.Fprintf(r.stderr, "herder-spawn: refusing to close root pane — terminal_id matches the agent (%s)\n", termID)
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
				fmt.Fprintf(r.stderr, "herder-spawn: skipped root-pane close — %s no longer holds terminal %s (now %s)\n", rootPaneID, rootTerm, now)
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
		out, rc := runSend(sendAbs, paneID, wirePayload)
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
		fmt.Fprintf(r.stderr, "  notify: rings %s on done (HERDER_SEND + HERDER_NOTIFY_TO injected)\n", r.opts.NotifyTo)
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
			fmt.Fprintf(r.stderr, "          Accept it in the pane (focus + Enter), then: herder-send %s \"<prompt>\"\n", record.Label)
		} else {
			fmt.Fprintf(r.stderr, "  prompt: NOT confirmed (verify: %s, ready: %s) — read the pane before assuming it landed\n", deliveryResult, readyReason)
		}
	} else if trustBlocked {
		fmt.Fprintln(r.stderr, "  note:   directory-trust modal is open (--safe); accept it in the pane to use the agent")
	}
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder-spawn: %s\n", msg)
}

func printHelp(stdout io.Writer) {
	lines := []string{
		"# herder-spawn — spawn a named, GUID-tagged agent in a herdr pane and register it.",
		"#",
		"# Usage:",
		"#   herder-spawn --role <role> --agent <claude|codex|bash|...> [--prompt TEXT | --prompt-file FILE]",
		"#                [--split right|down] [--workspace ID | --from-pane PANE_ID] [--tab ID | --new-tab] [--cwd PATH]",
		"#                [--team NAME] [--no-focus] [--safe] [--notify | --notify-to PANE] [--label-prefix STR] [--extra-arg ARG]... [--json]",
		"#",
		"# Launch-through-hcom + teams:",
		"#   * hcom-capable agents (claude/codex/gemini) are launched THROUGH hcom via the",
		"#     `hcom-launch` wrapper (`hcom <tool> --run-here --tag <role> ...`), so they bind to the message",
		"#     bus from birth (hooks + pty). hcom is a HARD dependency for these agents (fails early if",
		"#     absent). Non-hcom agents (bash, ...) exec raw and stay reachable over herdr keystrokes only.",
		"#   * TEAM = a named bus (ringfencing). HCOM_DIR = $HERDER_TEAMS_ROOT/<team> (default root",
		"#     ~/.hcom/teams); no --team = the global ~/.hcom bus (frictionless default for standing agents).",
		"#     Team resolves from --team, else $HERDER_TEAM (the orchestrator's team), else global. The",
		"#     resolved HCOM_DIR is PINNED into the child's PROCESS env at launch so it joins exactly this bus;",
		"#     the wrapper's config-dir passthrough keeps auth on the REAL config dir even for an isolated team.",
		"#   * The role is passed as the hcom `--tag`, so hcom names the instance `<role>-<random>` and",
		"#     `@<role>-` fan-out addressing works. herder-spawn CAPTURES the assigned name (by correlating the",
		"#     child's herdr pane_id to the hcom entry) and records `team`/`hcom_dir`/`hcom_name`/`hcom_tag` in",
		"#     the registry — herder-send/list/wait/cull resolve GUID/label to that bus coordinate.",
		"#   * Name capture is BEST-EFFORT: if it fails the spawn still succeeds and the agent stays reachable",
		"#     over herdr keystrokes; the outcome is reported in the summary and the --json `hcom_capture` field.",
		"#",
		"# Tab placement:",
		"#   * --tab <id>  places the agent in an existing tab (as a split alongside whatever is there).",
		"#   * --new-tab   gives the agent its own fresh tab with NO spare shell. `herdr tab create` always",
		"#                 seeds a tab with a default shell (root) pane, and `herdr agent start` always opens",
		"#                 a NEW pane (even without --split), so a naive \"tab create + agent start --tab\"",
		"#                 leaves the root shell behind. --new-tab creates the tab, spawns the agent into it,",
		"#                 then closes the root shell (verifying its terminal_id first so it never closes the",
		"#                 agent) and re-resolves the agent's pane_id by terminal_id after the close compacts",
		"#                 ids. The tab is labelled with the agent's label. --new-tab and --tab are exclusive.",
		"#",
		"# Behavior:",
		"#   1. Generates a short GUID and a label \"<role>-<shortguid>\".",
		"#   2. PERMISSIONS: by default spawns agents in autonomous (\"YOLO\") mode so they don't stall on",
		"#      approval prompts the herder can't see — claude gets --dangerously-skip-permissions, codex gets",
		"#      --dangerously-bypass-approvals-and-sandbox. This is injected because `exec claude` bypasses the",
		"#      user's shell alias (which is where skip-permissions usually lives), so spawned agents would",
		"#      otherwise come up in ask-mode. Pass --safe to opt out, or pass your own permission flag via",
		"#      --extra-arg (any recognised one suppresses the default).",
		"#   3. Calls `herdr agent start <label> ...`, launching the agent inside a login+interactive shell —",
		"#      `$SHELL -lic 'export HERDER_*...; exec <launch>'` — so PATH / mise activation / auth env are",
		"#      sourced like a normal pane (opt out with --no-login-shell). <launch> for an hcom agent is",
		"#      `hcom-launch <agent> --tag <role> [extra-args]` with HCOM_DIR pinned to the team bus; for a",
		"#      non-hcom agent it is the raw `<agent> [extra-args]`.",
		"#   4. After the agent settles, captures its hcom-assigned name and appends a JSONL record",
		"#      (incl. team/hcom_dir/hcom_name/hcom_tag) to $HERDER_STATE_DIR/registry.jsonl.",
		"#   5. If --prompt/--prompt-file is given, waits for the agent to become truly ready (sigil + idle +",
		"#      settle) then delegates delivery to `herder-send` (verified). For CODEX, a long or multi-line",
		"#      brief is staged to a file ($HERDER_STATE_DIR/briefs/<guid>.md) and only a one-line \"read it\"",
		"#      pointer is sent over the wire — this dodges codex's paste-blob / \"Create a plan?\" overlay /",
		"#      boot-clip pathologies entirely (see the staging block below). Claude gets the inline prompt.",
		"#   6. Prints a human summary to stderr and a JSON record to stdout when --json is set.",
		"#",
		"# Notes:",
		"#   * The agent binary must accept being invoked plainly (e.g. `codex`, `claude`). For wrappers, use --extra-arg.",
		"#   * Initial-prompt delivery is verified by `herder-send`; if it can't confirm submission it reports",
		"#     prompt: NOT confirmed rather than silently claiming success.",
		"#   * NOTIFY-BACK: every spawned agent gets HERDER_SEND (absolute path to herder-send) exported into",
		"#     its shell, so it can ring a peer regardless of $PATH or whether it loaded the herder skill —",
		"#     the gap that made agents fall back to raw `herdr agent send` (which writes text WITHOUT",
		"#     submitting it, so the ring silently never lands). --notify also exports HERDER_NOTIFY_TO (the",
		"#     spawner's pane, or --notify-to PANE) AND appends a concrete ring command to the prompt, so a",
		"#     finished worker rings the orchestrator instead of the orchestrator polling. The run-log block",
		"#     stays the record; the ring is only a doorbell.",
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

func runSend(sendAbs, paneID, payload string) ([]byte, int) {
	cmd := exec.Command(sendAbs, paneID, payload, "--json")
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

func findScriptDir() (string, error) {
	if root := os.Getenv("AI_CONFIG_ROOT"); root != "" {
		dir := filepath.Join(root, "skills", "herder", "scripts")
		if _, err := os.Stat(dir); err == nil {
			return dir, nil
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		dir := filepath.Join(wd, "skills", "herder", "scripts")
		if _, err := os.Stat(dir); err == nil {
			return dir, nil
		}
		next := filepath.Dir(wd)
		if next == wd {
			break
		}
		wd = next
	}
	return "", fmt.Errorf("could not locate skills/herder/scripts from current directory")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("@%_+=:,./-", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ':
			b.WriteString(`\ `)
		case '\n':
			b.WriteString(`\n`)
		case '\'':
			b.WriteString(`\'`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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
