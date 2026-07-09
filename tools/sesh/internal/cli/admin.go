package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"sesh/internal/index"
	"sesh/internal/store"
	"sesh/internal/wire"
)

func newAdmin() *cobra.Command {
	admin := &cobra.Command{
		Use:   "admin",
		Short: "Administrative operations on the store",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("%s: missing subcommand", cmd.CommandPath())
		},
	}
	admin.AddCommand(newDropFile())
	return admin
}

func newDropFile() *cobra.Command {
	var dataDir string
	var yes bool
	var reason string
	cmd := &cobra.Command{
		Use:   "drop-file <tool> <session_id> <file_uuid>",
		Short: "Drop one mirrored file identity from the store",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("refusing to drop without --yes")
			}
			var err error
			if dataDir == "" {
				dataDir, err = defaultStoreDir()
				if err != nil {
					return err
				}
			}
			st, err := store.Open(cmd.Context(), store.Config{Dir: dataDir, Logger: slog.Default()})
			if err != nil {
				return err
			}
			defer st.Close()
			if _, err := index.New(cmd.Context(), st.DB(), st.MirrorPath); err != nil {
				return err
			}
			if err := st.DropFile(cmd.Context(), wire.Tool(args[0]), args[1], args[2], reason); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("file identity not found")
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "dropped %s/%s/%s\n", args[0], args[1], args[2])
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "store data directory")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm irreversible drop")
	cmd.Flags().StringVar(&reason, "reason", "operator drop-file", "audit reason")
	return cmd
}
