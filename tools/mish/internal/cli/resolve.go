package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"mish/internal/resolve"

	"github.com/spf13/cobra"
)

// resolveOutput is the one-line JSON contract for machine consumers (mc,
// herder's join fallback, orchestrators). Success and refusal share the
// shape; ok discriminates. Refusals also land on stderr as prose and exit 1,
// so humans and shell pipelines both see them.
type resolveOutput struct {
	OK           bool     `json:"ok"`
	Slug         string   `json:"slug,omitempty"`
	MissionDir   string   `json:"mission_dir,omitempty"`
	Source       string   `json:"source,omitempty"`
	MarkerPath   string   `json:"marker_path,omitempty"`
	MissionsRepo string   `json:"missions_repo,omitempty"`
	Refusal      string   `json:"refusal,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Remedy       string   `json:"remedy,omitempty"`
	Paths        []string `json:"paths,omitempty"`
}

func newResolveCommand(d deps) *cobra.Command {
	var missionFlag string

	cmd := &cobra.Command{
		Use:          "resolve",
		Short:        "Print the resolved mission context as JSON",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{err: fmt.Errorf("mish resolve: unexpected arguments: %s — run 'mish resolve --help' for usage", strings.Join(args, " "))}
			}
			return runResolve(cmd, d, missionFlag, cmd.Flags().Changed("mission"))
		},
	}
	attachHelp(cmd, resolveHelpText)
	cmd.Flags().StringVar(&missionFlag, "mission", "", "mission slug to resolve")
	return cmd
}

func runResolve(cmd *cobra.Command, d deps, missionFlag string, missionFlagSet bool) error {
	cwd, err := d.cwd()
	if err != nil {
		return refusalError{verb: "resolve", message: "could not determine current directory", remedy: err.Error()}
	}
	result, err := resolve.Resolve(resolve.Options{
		MissionFlagSet: missionFlagSet,
		MissionFlag:    missionFlag,
		CWD:            cwd,
		Env: func(key string) string {
			if key == "MISSIONS_REPO" {
				return d.missionsRepo
			}
			return d.env(key)
		},
		FS: resolve.OSFS{},
	})
	if err != nil {
		var refusal *resolve.Refusal
		if errors.As(err, &refusal) {
			emitResolveJSON(cmd, resolveOutput{
				OK:      false,
				Refusal: string(refusal.Kind),
				Slug:    refusal.Slug,
				Reason:  refusal.Reason,
				Remedy:  refusal.Remedy,
				Paths:   refusal.Paths,
			})
			return refusalError{verb: "resolve", message: refusal.Reason, remedy: refusal.Remedy}
		}
		return refusalError{verb: "resolve", message: err.Error()}
	}
	emitResolveJSON(cmd, resolveOutput{
		OK:           true,
		Slug:         result.Slug,
		MissionDir:   result.MissionDir,
		Source:       string(result.Source),
		MarkerPath:   result.MarkerPath,
		MissionsRepo: d.missionsRepo,
	})
	return nil
}

func emitResolveJSON(cmd *cobra.Command, out resolveOutput) {
	encoded, err := json.Marshal(out)
	if err != nil {
		// Marshal of a plain struct cannot realistically fail; keep the
		// contract of always emitting one line.
		fmt.Fprintf(cmd.OutOrStdout(), `{"ok":false,"refusal":"encoding_error"}`+"\n")
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
}
