package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"sesh/internal/httpx"
	"sesh/internal/setup"
	"sesh/internal/ship"
)

func newStatus() *cobra.Command {
	var stateDir string
	var storeURL string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report shipper/store health, staleness, and quarantine state",
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if stateDir == "" {
				stateDir, err = ship.StateDir()
				if err != nil {
					return err
				}
			}
			if storeURL == "" {
				storeURL = os.Getenv("SESH_STORE_URL")
			}
			configPath := ""
			if storeURL == "" {
				// Interactive shells don't carry SESH_STORE_URL — the URL
				// lives in the installed service config only (the launchd
				// plist on macOS, the systemd drop-in on Linux). Resolve it
				// the way `sesh update` does so status stops reporting a
				// correctly-installed node as "not configured".
				if home, herr := os.UserHomeDir(); herr == nil {
					var url string
					var ok bool
					if url, configPath, ok = setup.InstalledStoreURL(runtime.GOOS, home); ok {
						storeURL = url
					}
				}
			}
			cursors, err := ship.LoadSnapshot(stateDir)
			if err != nil {
				return err
			}
			sort.Slice(cursors, func(i, j int) bool {
				return cursors[i].Identity().Key() < cursors[j].Identity().Key()
			})
			fmt.Fprintf(cmd.OutOrStdout(), "cursors: %d\n", len(cursors))
			poisoned := 0
			var newestAck time.Time
			for _, c := range cursors {
				if c.Poisoned {
					poisoned++
				}
				if c.LastAckAt.After(newestAck) {
					newestAck = c.LastAckAt
				}
				ack := "never"
				if !c.LastAckAt.IsZero() {
					ack = time.Since(c.LastAckAt).Round(time.Second).String() + " ago"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "- %s offset=%d last_ack=%s", c.Identity().Key(), c.Offset, ack)
				if c.Poisoned {
					fmt.Fprint(cmd.OutOrStdout(), " POISONED")
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			if newestAck.IsZero() {
				fmt.Fprintln(cmd.OutOrStdout(), "last_ack_age: never")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "last_ack_age: %s\n", time.Since(newestAck).Round(time.Second))
			}
			if storeURL != "" {
				if err := pingStore(cmd.Context(), storeURL); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "store: unreachable (%v)\n", err)
					return fmt.Errorf("store unreachable: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "store: reachable")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "store: not configured (no --store-url or SESH_STORE_URL, and %s carries none)\n", configPath)
			}
			if poisoned > 0 {
				return fmt.Errorf("%d poisoned file(s)", poisoned)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "shipper state directory")
	cmd.Flags().StringVar(&storeURL, "store-url", "", "store base URL (env: SESH_STORE_URL)")
	return cmd
}

// pingClient is bounded, never http.DefaultClient: an interactive status
// must fail within seconds when the store stalls, not hang the terminal.
var pingClient = httpx.NewClient(15*time.Second, 2)

func pingStore(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/health", nil)
	if err != nil {
		return err
	}
	resp, err := pingClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
