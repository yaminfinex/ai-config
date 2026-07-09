// Package cli wires the mish command tree.
package cli

import (
	"errors"
	"fmt"
	"io"

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
	d := newDeps(stdout, stderr)
	root := newRoot(d)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var refusal refusalError
		if errors.As(err, &refusal) {
			fmt.Fprintln(stderr, err)
			return exitRefuse
		}
		var usage usageError
		if errors.As(err, &usage) {
			fmt.Fprintln(stderr, err)
			return exitUsage
		}
		fmt.Fprintf(stderr, "mish: %v \u2014 run 'mish --help' for the command list\n", err)
		return exitUsage
	}
	return exitOK
}

func newRoot(d deps) *cobra.Command {
	root := &cobra.Command{
		Use:           commandName,
		Short:         "mish manages mission directories and their Backlog.md boards",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(d.stdout)
	root.SetErr(d.stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err: fmt.Errorf("mish: %w \u2014 run 'mish --help' for the command list", err)}
	})
	root.AddCommand(
		newNewCommand(),
		newBacklogCommand(),
		newStatusCommand(),
	)
	return root
}

func newNewCommand() *cobra.Command {
	return stubCommand("new", "Scaffold a mission directory")
}

func newBacklogCommand() *cobra.Command {
	return stubCommand("backlog", "Run an allowlisted Backlog.md command inside a mission")
}

func newStatusCommand() *cobra.Command {
	return stubCommand("status", "Report mission health without mutating files")
}

func stubCommand(name, short string) *cobra.Command {
	return &cobra.Command{
		Use:          name,
		Short:        short,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return refusalError{
				verb:    name,
				message: "not implemented in this bootstrap unit",
				remedy:  "run 'mish --help' for the command list",
			}
		},
	}
}
