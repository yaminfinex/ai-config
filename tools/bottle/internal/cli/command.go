package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"ai-config/tools/bottle/internal/harness/claude"
	"ai-config/tools/bottle/internal/store"
)

// deps is everything a management command needs to run, injected so tests can
// drive a temp store and assert on buffers without touching ~/.bottles,
// os.Stdout, or a real TTY. The `live` wrapper builds the production deps; the
// command funcs (cmdList, cmdLog, …) take *deps and never reach for globals.
type deps struct {
	store  *store.Store
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	now    func() time.Time
	// isTTY reports whether stdin is an interactive terminal — gates rm's
	// y/N prompt vs its --force-required refusal.
	isTTY bool
	// sessionExists is the prune seam: given a decant's bottle meta and its
	// session id, does the underlying harness session file still exist? U5
	// owns the real locator (see defaultSessionExists).
	sessionExists func(meta *store.Meta, sessionID string) bool

	// The remaining fields drive the U6 core verbs (create/decant/rebottle).

	// cwd is the directory bottle runs in — where the live session file lives
	// (create/rebottle locate <session>.jsonl under its encoded project dir).
	cwd string
	// projectsRoot is Claude's session store root ($HOME/.claude/projects);
	// tests point it at a temp dir.
	projectsRoot string
	// selfSession is the live session's id ($CLAUDE_CODE_SESSION_ID): bottling
	// it triggers the self-bottle trim, and rebottle defaults to it.
	selfSession string
	// launch executes a decant's LaunchPlan (chdir + exec). Injected so decant
	// tests assert the plan without spawning a harness; a nil error from the
	// production impl never returns (exec replaces the process).
	launch func(plan claude.LaunchPlan) error
	// gitInfo reads the branch and short sha of cwd for create-time provenance
	// (best-effort, both "" outside a repo). A seam so create tests stay fast
	// and deterministic without spawning git.
	gitInfo func(cwd string) (branch, sha string)
}

// live builds a real (non-stub) command. `--help`/`-h` prints the agent-tuned
// help (usage + summary + examples + pitfalls) and exits 0 without opening the
// store, keeping help cheap; any real invocation opens the default ~/.bottles
// store and dispatches to fn with production deps. Tests bypass this and call
// the cmd* funcs directly with a temp-store *deps.
func live(name, usage, summary, help string, fn func(d *deps, args []string) int) command {
	return command{
		name:    name,
		usage:   usage,
		summary: summary,
		help:    help,
		run: func(args []string, stdout, stderr io.Writer) int {
			for _, arg := range args {
				if arg == "-h" || arg == "--help" {
					fmt.Fprint(stdout, renderHelp(usage, summary, help))
					return 0
				}
			}
			root, err := store.DefaultRoot()
			if err != nil {
				fmt.Fprintf(stderr, "bottle %s: %v\n", name, err)
				return 1
			}
			st, err := store.Open(root)
			if err != nil {
				fmt.Fprintf(stderr, "bottle %s: %v\n", name, err)
				return 1
			}
			cwd, _ := os.Getwd()
			d := &deps{
				store:         st,
				stdin:         os.Stdin,
				stdout:        stdout,
				stderr:        stderr,
				now:           time.Now,
				isTTY:         stdinIsTTY(os.Stdin),
				sessionExists: defaultSessionExists,
				cwd:           cwd,
				projectsRoot:  defaultProjectsRoot(),
				selfSession:   claude.SelfSessionID(),
				launch:        execLaunch,
				gitInfo:       realGitInfo,
			}
			return fn(d, args)
		},
	}
}

// defaultSessionExists is the prune seam's production locator: a decant's seed
// lives at <projectsRoot>/<encoded meta.Source.CWD>/<session>.jsonl (the same
// place Materialize wrote it). When the projects root can't be resolved, prune
// treats the decant as live rather than deleting a registry entry on a guess.
func defaultSessionExists(meta *store.Meta, sessionID string) bool {
	root := defaultProjectsRoot()
	if root == "" {
		return true
	}
	seed := filepath.Join(claude.ProjectDir(root, meta.Source.CWD), sessionID+".jsonl")
	_, err := os.Stat(seed)
	return err == nil
}

// defaultProjectsRoot is Claude's session store, $HOME/.claude/projects. It
// returns "" when the home directory can't be located, leaving callers to
// degrade gracefully.
func defaultProjectsRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// realGitInfo reads cwd's current branch and HEAD sha (best-effort). Any error
// — not a repo, git absent, detached weirdness — yields empty strings; git
// state is recorded for display/provenance only, never load-bearing.
func realGitInfo(cwd string) (branch, sha string) {
	if cwd == "" {
		return "", ""
	}
	run := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", cwd}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return run("rev-parse", "--abbrev-ref", "HEAD"), run("rev-parse", "--short", "HEAD")
}

// execLaunch is the production decant launcher: chdir to the plan's run cwd
// (resume is cwd-scoped, so the chdir is mandatory) then exec the argv,
// replacing this process. It returns only on failure before the exec.
func execLaunch(plan claude.LaunchPlan) error {
	if len(plan.Argv) == 0 {
		return fmt.Errorf("launch: empty argv")
	}
	if err := os.Chdir(plan.RunCwd); err != nil {
		return fmt.Errorf("chdir %s: %w", plan.RunCwd, err)
	}
	bin, err := resolveLaunchBin(plan.Argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(bin, plan.Argv, os.Environ())
}

// resolveLaunchBin locates argv0 via $PATH. Pane decants rely on the same
// ai-setup/mise PATH contract as the herder CLI itself.
func resolveLaunchBin(argv0 string) (string, error) {
	if p, lookErr := exec.LookPath(argv0); lookErr == nil {
		return p, nil
	}
	return "", fmt.Errorf("locate %s: not on $PATH", argv0)
}

// stdinIsTTY reports whether stdin is a character device (an interactive
// terminal). Stdlib-only: no golang.org/x/term dependency.
func stdinIsTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// parseFlexible parses a FlagSet while tolerating flags that appear after
// positional arguments (the spec writes `bottle show <name> [--turns N]`, but
// stdlib flag.Parse stops at the first non-flag token). It repeatedly parses,
// peels off one positional, and parses again, so flags and positionals may
// interleave. Flags that take a value still consume their value correctly
// because the FlagSet knows their arity.
func parseFlexible(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// humanizeAge renders a duration as a compact age (5s, 3m, 2h, 4d) for list.
func humanizeAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// shortSession abbreviates a session UUID for log/show display.
func shortSession(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8] + "…"
}

// oneLine flattens whitespace and truncates to at most max runes, for a
// single-line transcript turn preview.
func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
