// Package cli wires the mish command tree.
package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

const (
	exitOK      = 0
	exitRefuse  = 1
	exitUsage   = 2
	commandName = "mish"
)

type usageError struct {
	err error
}

type passthroughExit struct {
	code int
}

func (e passthroughExit) Error() string {
	return fmt.Sprintf("passthrough exited %d", e.code)
}

func (e usageError) Error() string {
	return e.err.Error()
}

type refusalError struct {
	verb    string
	message string
	remedy  string
}

func (e refusalError) Error() string {
	if e.remedy == "" {
		return fmt.Sprintf("mish %s: %s", e.verb, e.message)
	}
	return fmt.Sprintf("mish %s: %s \u2014 %s", e.verb, e.message, e.remedy)
}

// Run executes the mish command tree and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return runWithDeps(args, newDeps(stdout, stderr))
}

func runWithDeps(args []string, d deps) int {
	root := newRoot(d)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var passthrough passthroughExit
		if errors.As(err, &passthrough) {
			return passthrough.code
		}
		var refusal refusalError
		if errors.As(err, &refusal) {
			fmt.Fprintln(d.stderr, err)
			return exitRefuse
		}
		var usage usageError
		if errors.As(err, &usage) {
			fmt.Fprintln(d.stderr, err)
			return exitUsage
		}
		fmt.Fprintf(d.stderr, "mish: %v \u2014 run 'mish --help' for the command list\n", err)
		return exitUsage
	}
	return exitOK
}

func newRoot(d deps) *cobra.Command {
	root := &cobra.Command{
		Use:           commandName,
		Short:         "mish manages mission directories and their Backlog.md boards",
		Long:          strings.TrimSuffix(rootHelp(), "\n"),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(d.stdout)
	root.SetErr(d.stderr)
	attachHelp(root, rootHelp())
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err: fmt.Errorf("mish: %w \u2014 run 'mish --help' for the command list", err)}
	})
	root.AddCommand(
		newNewCommand(d),
		newBacklogCommand(d),
		newStatusCommand(d),
	)
	return root
}
