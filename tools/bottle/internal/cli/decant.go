package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ai-config/tools/bottle/internal/harness/claude"
	"ai-config/tools/bottle/internal/refs"
)

// cmdDecant materializes a fresh, resumable session from a bottle and launches
// it. The decant runs in the CURRENT directory (or --cwd) — the bottle's
// recorded cwd is provenance, not a destination; a note prints when they
// differ. The order is load-bearing: resolve the ref, validate the run cwd
// BEFORE materializing (so a bad --cwd never leaves an orphan seed),
// materialize the seed, record the decants-map entry (after a successful
// materialize, before launch), then build and exec the launch plan —
// interactively in this terminal or into a herdr split with --pane.
func cmdDecant(d *deps, args []string) int {
	fs := flag.NewFlagSet("decant", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	pane := fs.String("pane", "", "launch into a herdr split: right or below")
	prompt := fs.String("prompt", "", "initial prompt for the decanted session")
	yolo := fs.Bool("yolo", false, "skip permission prompts (--dangerously-skip-permissions)")
	cwdOverride := fs.String("cwd", "", "run the decant in this directory instead of the current one")
	pos, err := parseFlexible(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) < 1 {
		fmt.Fprintln(d.stderr, "Usage: bottle decant <name>[@v] [--pane right|below] [--prompt ...] [--yolo] [--cwd PATH]")
		return 2
	}
	ref, err := refs.Parse(pos[0])
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle decant: %v\n", err)
		return 1
	}
	paneMode, err := parsePane(*pane)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle decant: %v\n", err)
		return 2
	}
	b, err := d.store.Resolve(ref)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle decant: %v\n", err)
		return 1
	}

	runCwd := d.cwd
	if *cwdOverride != "" {
		runCwd = *cwdOverride
	}
	// Validate the run cwd before materializing — BuildLaunch also refuses a
	// dead cwd, but materialize writes the seed first, so checking here keeps a
	// bad --cwd from leaving an orphan seed (U5 orphan-seed ordering rule).
	if fi, err := os.Stat(runCwd); err != nil || !fi.IsDir() {
		fmt.Fprintf(d.stderr, "bottle decant: cwd %s does not exist\n", runCwd)
		return 1
	}
	// Canonicalize before deriving the seed's project directory: Claude resumes
	// by looking up the encoded form of its *physical* working directory, so a
	// relative --cwd (".") or a symlinked component would seed a project dir
	// Claude never checks ("No conversation found").
	runCwd, err = canonicalCwd(runCwd)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle decant: resolve cwd: %v\n", err)
		return 1
	}

	// Materialize from a temp copy of the frozen transcript, keeping decant
	// agnostic to the store's on-disk layout.
	srcPath, cleanup, err := d.tempTranscript(b.ID)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle decant: %v\n", err)
		return 1
	}
	defer cleanup()

	res, err := claude.Materialize(claude.MaterializeRequest{
		SourcePath:   srcPath,
		ProjectsRoot: d.projectsRoot,
		Cwd:          runCwd,
	})
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle decant: %v\n", err)
		return 1
	}

	// Record the decant only after a successful materialize, before launch.
	if err := d.store.RecordDecant(res.SessionID, b.ID); err != nil {
		fmt.Fprintf(d.stderr, "bottle decant: %v\n", err)
		return 1
	}

	plan, err := claude.BuildLaunch(claude.LaunchRequest{
		SessionID:      res.SessionID,
		Cwd:            runCwd,
		Pane:           paneMode,
		Prompt:         *prompt,
		Yolo:           *yolo,
		PermissionMode: res.PermissionMode,
	})
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle decant: %v\n", err)
		return 1
	}

	fmt.Fprintf(d.stdout, "Decanted %s@%d → session %s (cwd %s)\n",
		b.Meta.Name, b.Meta.Version, shortSession(res.SessionID), runCwd)
	if rec := b.Meta.Source.CWD; rec != "" && rec != runCwd {
		fmt.Fprintf(d.stdout, "note: bottled in %s — the conversation's file references assume that tree\n", rec)
	}
	if plan.Pane {
		fmt.Fprintf(d.stdout, "launching pane: %s\n", strings.Join(plan.Argv, " "))
	}
	if err := d.launch(plan); err != nil {
		fmt.Fprintf(d.stderr, "bottle decant: launch failed: %v\n", err)
		return 1
	}
	return 0
}

// canonicalCwd resolves cwd to the absolute, symlink-free path — the same form
// Claude derives from getcwd after the launch chdirs there. The seed must land
// in the project dir encoded from THAT path or resume can't find it.
func canonicalCwd(cwd string) (string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// parsePane validates the --pane value and maps it to a claude pane constant
// (the strings already match: PaneNone/Right/Below).
func parsePane(v string) (string, error) {
	switch v {
	case claude.PaneNone, claude.PaneRight, claude.PaneBelow:
		return v, nil
	default:
		return "", fmt.Errorf("--pane must be %q or %q, got %q", claude.PaneRight, claude.PaneBelow, v)
	}
}

// tempTranscript writes a bottle's frozen transcript to a temp file and returns
// its path plus a cleanup func, so Materialize can read it as an ordinary file
// without decant reaching into the store's directory layout.
func (d *deps) tempTranscript(bottleID string) (path string, cleanup func(), err error) {
	data, err := d.store.ReadTranscript(bottleID)
	if err != nil {
		return "", nil, err
	}
	tmp, err := os.CreateTemp("", "bottle-decant-src-*.jsonl")
	if err != nil {
		return "", nil, err
	}
	name := tmp.Name()
	cleanup = func() { os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return name, cleanup, nil
}
