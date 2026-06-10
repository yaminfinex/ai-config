package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

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
}

// live builds a real (non-stub) command. `--help`/`-h` prints the one-line
// usage and exits 0 without opening the store (matching stub's contract and
// keeping help cheap); any real invocation opens the default ~/.bottles store
// and dispatches to fn with production deps. Tests bypass this and call the
// cmd* funcs directly with a temp-store *deps.
func live(name, usage, summary string, fn func(d *deps, args []string) int) command {
	return command{
		name:    name,
		usage:   usage,
		summary: summary,
		run: func(args []string, stdout, stderr io.Writer) int {
			for _, arg := range args {
				if arg == "-h" || arg == "--help" {
					fmt.Fprintf(stdout, "Usage: %s\n\n%s.\n", usage, summary)
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
			d := &deps{
				store:         st,
				stdin:         os.Stdin,
				stdout:        stdout,
				stderr:        stderr,
				now:           time.Now,
				isTTY:         stdinIsTTY(os.Stdin),
				sessionExists: defaultSessionExists,
			}
			return fn(d, args)
		},
	}
}

// defaultSessionExists is the prune seam's production stand-in. U5
// (harness/claude) owns the real locator — cwd (from meta.Source.CWD) →
// encoded project dir → <session>.jsonl — and is being built in parallel.
// Until it lands, prune treats every decant as live so it never deletes a
// registry entry on a guess.
//
// TODO(U6/U8): wire the harness/claude session-file locator here so prune can
// detect GC'd decant seeds.
func defaultSessionExists(meta *store.Meta, sessionID string) bool { return true }

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
