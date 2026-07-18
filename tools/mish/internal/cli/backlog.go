package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mish/internal/resolve"

	"github.com/spf13/cobra"
)

const backlogBinary = "backlog"

func newBacklogCommand(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:                "backlog [--mission <slug>] <subcommand> [args...]",
		Short:              "Run an allowlisted Backlog.md command inside a mission",
		SilenceUsage:       true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBacklog(d, args)
		},
	}
	attachHelp(cmd, backlogHelp())
	return cmd
}

func runBacklog(d deps, args []string) error {
	parsed, err := parseBacklogArgs(args)
	if err != nil {
		return err
	}
	if parsed.help || len(parsed.tail) == 0 {
		_, err := fmt.Fprint(d.stdout, backlogHelp())
		return err
	}

	cwd, err := d.cwd()
	if err != nil {
		return refusalError{
			verb:    "backlog",
			kind:    "cwd_unavailable",
			message: "could not determine current directory",
			remedy:  "run from a readable directory or pass --mission <slug>",
			text:    parsed.text,
		}
	}
	result, err := resolve.Resolve(resolve.Options{
		MissionFlagSet: parsed.missionFlagSet,
		MissionFlag:    parsed.missionFlag,
		CWD:            cwd,
		Env: func(key string) string {
			if key == "MISSIONS_REPO" {
				return d.missionsRepo
			}
			if d.env == nil {
				return ""
			}
			return d.env(key)
		},
	})
	if err != nil {
		return backlogRefusalFromResolve(err, parsed.text)
	}
	if err := requireMissionBoard(result.MissionDir); err != nil {
		return withRefusalText(err, parsed.text)
	}
	subcommand := parsed.tail[0]
	if !isBacklogAllowed(subcommand) {
		return refusalError{
			verb:    "backlog",
			kind:    "subcommand_not_allowed",
			message: fmt.Sprintf("subcommand %q is not allowed", subcommand),
			remedy:  "use one of: " + backlogAllowlistSummary(),
			text:    parsed.text,
		}
	}
	if _, err := d.lookPath(backlogBinary); err != nil {
		return refusalError{
			verb:    "backlog",
			kind:    "backlog_cli_not_found",
			message: "Backlog.md CLI not found",
			remedy:  "install npm:backlog.md 1.47.x with mise or put 'backlog' on PATH",
			text:    parsed.text,
		}
	}

	execResult := d.exec(backlogBinary, parsed.tail, result.MissionDir, d.stdin, d.stdout, d.stderr)
	if execResult.ExitCode == 0 {
		return nil
	}
	return passthroughExit{code: execResult.ExitCode}
}

type parsedBacklogArgs struct {
	missionFlagSet bool
	missionFlag    string
	tail           []string
	help           bool
	text           bool
}

func parseBacklogArgs(args []string) (parsedBacklogArgs, error) {
	if len(args) == 0 || isWrapperBacklogHelp(args[0]) {
		return parsedBacklogArgs{help: true}, nil
	}
	var parsed parsedBacklogArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--mission=") {
			parsed.missionFlagSet = true
			parsed.missionFlag = strings.TrimPrefix(arg, "--mission=")
			continue
		}
		if arg == "--mission" {
			if i+1 >= len(args) {
				return parsedBacklogArgs{}, usageError{err: fmt.Errorf("mish backlog: --mission needs a slug")}
			}
			parsed.missionFlagSet = true
			parsed.missionFlag = args[i+1]
			i++
			continue
		}
		if arg == "--text" {
			parsed.text = true
			continue
		}
		if isWrapperBacklogHelp(arg) {
			parsed.help = true
			continue
		}
		parsed.tail = args[i:]
		return parsed, nil
	}
	return parsed, nil
}

func isWrapperBacklogHelp(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

func requireMissionBoard(missionDir string) error {
	path := filepath.Join(missionDir, "backlog", "config.yml")
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return nil
	}
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return refusalError{
			verb:    "backlog",
			kind:    "board_missing",
			message: "board missing",
			remedy:  "scaffold damaged or wrong mission",
		}
	}
	return refusalError{
		verb:    "backlog",
		kind:    "board_inspection_failed",
		message: fmt.Sprintf("could not inspect board %s", path),
		remedy:  "fix filesystem permissions and retry",
	}
}

func backlogRefusalFromResolve(err error, text bool) error {
	var refusal *resolve.Refusal
	if errors.As(err, &refusal) {
		return refusalError{
			verb:    "backlog",
			kind:    string(refusal.Kind),
			message: refusal.Reason,
			remedy:  refusal.Remedy,
			slug:    refusal.Slug,
			paths:   refusal.Paths,
			text:    text,
		}
	}
	return refusalError{
		verb:    "backlog",
		kind:    "resolution_failed",
		message: err.Error(),
		remedy:  "pass --mission <slug>, run from inside missions/<slug>/, or add a .mission marker",
		text:    text,
	}
}
