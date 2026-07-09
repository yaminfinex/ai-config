package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mish/internal/missionfs"
	"mish/internal/resolve"

	"github.com/spf13/cobra"
)

func newStatusCommand(d deps) *cobra.Command {
	var missionFlag string
	var all bool

	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Report mission health without mutating files",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{err: fmt.Errorf("mish status: unexpected arguments: %s — run 'mish status --help' for usage", strings.Join(args, " "))}
			}
			if all {
				return refusalError{
					verb:    "status",
					message: "overview mode is not implemented in this unit",
					remedy:  "run from inside a mission or pass --mission <slug>",
				}
			}
			return runStatus(cmd, d, missionFlag, cmd.Flags().Changed("mission"))
		},
	}
	cmd.Flags().StringVar(&missionFlag, "mission", "", "mission slug to report")
	cmd.Flags().BoolVar(&all, "all", false, "report all missions")
	return cmd
}

func runStatus(cmd *cobra.Command, d deps, missionFlag string, missionFlagSet bool) error {
	cwd, err := d.cwd()
	if err != nil {
		return refusalError{verb: "status", message: "could not determine current directory", remedy: err.Error()}
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
			return refusalError{verb: "status", message: refusal.Reason, remedy: refusal.Remedy}
		}
		return err
	}

	report, err := buildStatusReport(d, result)
	if err != nil {
		return refusalError{
			verb:    "status",
			message: "could not read mission status",
			remedy:  err.Error(),
		}
	}
	fmt.Fprint(cmd.OutOrStdout(), report)
	return nil
}

type statusReport struct {
	Manifest  missionfs.Manifest
	Counts    []missionfs.StatusCount
	Artifacts missionfs.ArtifactScan
	Warnings  []string
	BoardOK   bool
}

func buildStatusReport(d deps, result resolve.Result) (string, error) {
	report, err := collectStatus(d, result)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "mission: %s         %s     authority: %s   owner: %s   created %s\n",
		result.Slug,
		valueOrUnknown(report.Manifest.Status),
		valueOrUnknown(report.Manifest.Authority),
		valueOrUnknown(report.Manifest.Owner),
		valueOrUnknown(report.Manifest.Created),
	)
	if report.BoardOK {
		fmt.Fprintf(&b, "board:   %s   (%d tasks)\n", formatCounts(report.Counts), totalTasks(report.Counts))
	} else {
		fmt.Fprintf(&b, "board:   %s\n", formatCounts(report.Counts))
	}
	fmt.Fprintf(&b, "artifacts: %s\n", formatArtifacts(report.Artifacts, d.clock()))
	for _, warning := range report.Warnings {
		fmt.Fprintf(&b, "warning: %s\n", warning)
	}
	return b.String(), nil
}

func collectStatus(d deps, result resolve.Result) (statusReport, error) {
	var report statusReport
	var findings []missionfs.Finding

	manifest, manifestFindings, err := missionfs.ReadManifest(result.MissionDir)
	if err != nil {
		return statusReport{}, fmt.Errorf("read manifest: %w", err)
	}
	report.Manifest = manifest
	findings = append(findings, manifestFindings...)

	boardDir := filepath.Join(result.MissionDir, "backlog")
	cfg, boardFindings, err := missionfs.ReadBoardConfig(boardDir)
	if err != nil {
		return statusReport{}, err
	}
	findings = append(findings, boardFindings...)
	if !cfg.Missing && len(cfg.Statuses) > 0 {
		report.BoardOK = true
		taskScan, err := missionfs.ScanTasks(boardDir)
		if err != nil {
			return statusReport{}, err
		}
		orderedCounts, orderedFindings := taskScan.OrderedCounts(cfg.Statuses)
		report.Counts = orderedCounts
		findings = append(findings, taskScan.Findings...)
		findings = append(findings, orderedFindings...)
	}

	artifacts, err := missionfs.ScanArtifacts(filepath.Join(result.MissionDir, "artifacts"))
	if err != nil {
		return statusReport{}, err
	}
	report.Artifacts = artifacts
	findings = append(findings, artifacts.Findings...)

	report.Warnings = append(report.Warnings, formatFindings(result.MissionDir, findings)...)
	if dirty, err := missionHasStaleGit(d, result); err == nil && dirty {
		report.Warnings = append(report.Warnings, "mission subtree has uncommitted or unpushed changes")
	}
	return report, nil
}

func formatCounts(counts []missionfs.StatusCount) string {
	if len(counts) == 0 {
		return "unavailable"
	}
	parts := make([]string, 0, len(counts))
	for _, count := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", count.Count, count.Status))
	}
	return strings.Join(parts, " · ")
}

func totalTasks(counts []missionfs.StatusCount) int {
	total := 0
	for _, count := range counts {
		total += count.Count
	}
	return total
}

func formatArtifacts(artifacts missionfs.ArtifactScan, now time.Time) string {
	if artifacts.Missing {
		return "missing"
	}
	fileWord := "files"
	if artifacts.Count == 1 {
		fileWord = "file"
	}
	if artifacts.Count == 0 {
		return "0 files"
	}
	return fmt.Sprintf("%d %s · newest %s (%s ago)", artifacts.Count, fileWord, artifacts.NewestPath, ago(now, artifacts.NewestTime))
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func formatFindings(missionDir string, findings []missionfs.Finding) []string {
	warnings := make([]string, 0, len(findings))
	for _, finding := range findings {
		switch finding.Kind {
		case missionfs.FindingMalformedManifest:
			warnings = append(warnings, fmt.Sprintf("malformed mission.md frontmatter: %s", relativeFindingPath(missionDir, finding.Path)))
		case missionfs.FindingUnknownManifestKey:
			warnings = append(warnings, fmt.Sprintf("unknown mission.md frontmatter key: %s", finding.Key))
		case missionfs.FindingMissingManifestKey:
			warnings = append(warnings, fmt.Sprintf("missing mission.md frontmatter key: %s", finding.Key))
		case missionfs.FindingManifestSlugMismatch:
			warnings = append(warnings, fmt.Sprintf("mission frontmatter %q does not match directory %q", finding.Actual, finding.Expected))
		case missionfs.FindingInvalidManifestStatus:
			warnings = append(warnings, fmt.Sprintf("invalid mission status %q (expected active or closed)", finding.Actual))
		case missionfs.FindingMissingBoard:
			warnings = append(warnings, "board missing: backlog/config.yml")
		case missionfs.FindingMalformedBoardConfig:
			warnings = append(warnings, fmt.Sprintf("malformed board config: %s", relativeFindingPath(missionDir, finding.Path)))
		case missionfs.FindingBoardPinDrift:
			warnings = append(warnings, fmt.Sprintf("pinned board key drift: %s expected %s got %s", finding.Key, finding.Expected, finding.Actual))
		case missionfs.FindingMissingArtifacts:
			warnings = append(warnings, "artifacts missing: artifacts/")
		case missionfs.FindingMalformedTask:
			warnings = append(warnings, fmt.Sprintf("malformed task frontmatter: %s", relativeFindingPath(missionDir, finding.Path)))
		case missionfs.FindingMissingTaskID:
			warnings = append(warnings, fmt.Sprintf("task missing id: %s", relativeFindingPath(missionDir, finding.Path)))
		case missionfs.FindingUnknownTaskStatus:
			warnings = append(warnings, fmt.Sprintf("task status outside board config: %q", finding.Actual))
		case missionfs.FindingDuplicateTaskID:
			warnings = append(warnings, fmt.Sprintf("duplicate task ID %s: %s", finding.Actual, strings.Join(relativeFindingPaths(missionDir, finding.Paths), ", ")))
		}
	}
	return warnings
}

func relativeFindingPath(missionDir, path string) string {
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(missionDir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func relativeFindingPaths(missionDir string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, relativeFindingPath(missionDir, path))
	}
	return out
}

func missionHasStaleGit(d deps, result resolve.Result) (bool, error) {
	repo := d.missionsRepo
	if repo == "" {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		return false, nil
	}
	if out, err := d.git([]string{"rev-parse", "--is-inside-work-tree"}, repo); err != nil || strings.TrimSpace(string(out)) != "true" {
		return false, nil
	}
	if _, err := d.git([]string{"rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"}, repo); err != nil {
		return false, nil
	}
	pathspec, err := filepath.Rel(repo, result.MissionDir)
	if err != nil {
		return false, err
	}
	pathspec = filepath.ToSlash(pathspec)

	out, err := d.git([]string{"status", "--porcelain", "--", pathspec}, repo)
	if err != nil {
		return false, nil
	}
	if strings.TrimSpace(string(out)) != "" {
		return true, nil
	}
	out, err = d.git([]string{"rev-list", "--count", "@{u}..HEAD", "--", pathspec}, repo)
	if err != nil {
		return false, nil
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return false, nil
	}
	return count > 0, nil
}

func ago(now, then time.Time) string {
	if then.IsZero() {
		return "unknown"
	}
	if then.After(now) {
		return "0s"
	}
	d := now.Sub(then)
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
