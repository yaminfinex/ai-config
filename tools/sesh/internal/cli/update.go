package cli

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"sesh/internal/buildinfo"
	"sesh/internal/update"
)

// exitCodeError carries a specific process exit code through Execute to
// main. The message is empty: whatever needed saying was already printed.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return "" }

// ExitCode maps Execute's error to the process exit code (default 1).
func ExitCode(err error) int {
	var coded exitCodeError
	if errors.As(err, &coded) {
		return coded.code
	}
	if err != nil {
		return 1
	}
	return 0
}

func newUpdate() *cobra.Command {
	var storeURL string
	var check bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Converge this node's binary and running service to the store's latest release",
		Long: `Fetch the store's published latest version and, when it differs from this
build, download the matching binary, verify its SHA256SUMS entry, replace the
service's pinned executable crash-safely (the previous binary is retained as
sesh.prev and the target path is never missing at any point), restart the
unit, and verify the RUNNING image reports the new version.

Version semantics are equality-only: "latest differs" means "converge to
latest", so a deliberate latest rollback on the store propagates as a fleet
downgrade — visible, never silent (from -> to is always printed).

The base URL is the SESH_STORE_URL this node already couples on (the
installed drop-in on Linux, the launchd plist on macOS); --store-url
overrides it for pre-setup use.

Exit codes with --check (stable for scripting): 0 already up to date,
1 update available, 2 the check itself failed. Nothing is downloaded.`,
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := update.Run(update.Options{
				StoreURL: storeURL,
				Check:    check,
				OS:       runtime.GOOS,
				Arch:     runtime.GOARCH,
				Version:  buildinfo.Version,
				UID:      os.Getuid(),
				Out:      cmd.OutOrStdout(),
			})
			switch {
			case err == nil:
				return nil
			case errors.Is(err, update.ErrUpdateAvailable):
				return exitCodeError{code: 1}
			default:
				fmt.Fprintf(cmd.ErrOrStderr(), "sesh update: %v\n", err)
				if check {
					return exitCodeError{code: 2}
				}
				return exitCodeError{code: 1}
			}
		},
	}
	cmd.Flags().StringVar(&storeURL, "store-url", "", "store base URL override (default: the installed service config)")
	cmd.Flags().BoolVar(&check, "check", false, "report whether an update is available; download nothing")
	return cmd
}
