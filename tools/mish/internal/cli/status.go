package cli

import (
	"errors"
	"fmt"
	"io/fs"
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
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), report)
	return nil
}

type statusReport struct {
	Manifest  missionfs.Manifest
	Counts    []missionfs.StatusCount
	Artifacts artifactSummary
	Warnings  []string
}

type artifactSummary struct {
	Missing    bool
	Count      int
	NewestPath string
	NewestTime time.Time
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
	fmt.Fprintf(&b, "board:   %s   (%d tasks)\n", formatCounts(report.Counts), totalTasks(report.Counts))
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
	if !cfg.Missing {
		taskScan, err := missionfs.ScanTasks(boardDir)
		if err != nil {
			return statusReport{}, err
		}
		orderedCounts, orderedFindings := taskScan.OrderedCounts(cfg.Statuses)
		report.Counts = orderedCounts
		findings = append(findings, taskScan.Findings...)
		findings = append(findings, orderedFindings...)
	}

	artifacts, err := scanArtifactSummary(filepath.Join(result.MissionDir, "artifacts"), result.MissionDir)
	if err != nil {
		return statusReport{}, err
	}
	report.Artifacts = artifacts
	if artifacts.Missing {
		findings = append(findings, missionfs.Finding{Kind: missionfs.FindingKind("missing_artifacts"), Path: filepath.Join(result.MissionDir, "artifacts")})
	}

	report.Warnings = append(report.Warnings, formatFindings(findings)...)
	if dirty, err := missionHasStaleGit(d, result); err == nil && dirty {
		report.Warnings = append(report.Warnings, "mission subtree has uncommitted or unpushed changes")
	}
	return report, nil
}

func formatCounts(counts []missionfs.StatusCount) string {
	if len(counts) == 0 {
		return "missing board"
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

func formatArtifacts(artifacts artifactSummary, now time.Time) string {
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

func scanArtifactSummary(artifactsDir, missionDir string) (artifactSummary, error) {
	info, err := os.Stat(artifactsDir)
	if os.IsNotExist(err) {
		return artifactSummary{Missing: true}, nil
	}
	if err != nil {
		return artifactSummary{}, err
	}
	if !info.IsDir() {
		return artifactSummary{}, fmt.Errorf("artifacts path is not a directory: %s", artifactsDir)
	}

	var summary artifactSummary
	err = filepath.WalkDir(artifactsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		summary.Count++
		if info.ModTime().After(summary.NewestTime) {
			rel, err := filepath.Rel(filepath.Join(missionDir, "artifacts"), path)
			if err != nil {
				return err
			}
			summary.NewestPath = filepath.ToSlash(rel)
			summary.NewestTime = info.ModTime()
		}
		return nil
	})
	return summary, err
}

func formatFindings(findings []missionfs.Finding) []string {
	warnings := make([]string, 0, len(findings))
	for _, finding := range findings {
		switch finding.Kind {
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
		case missionfs.FindingBoardPinDrift:
			warnings = append(warnings, fmt.Sprintf("pinned board key drift: %s expected %s got %s", finding.Key, finding.Expected, finding.Actual))
		case missionfs.FindingMalformedTask:
			warnings = append(warnings, fmt.Sprintf("malformed task frontmatter: %s", finding.Path))
		case missionfs.FindingMissingTaskID:
			warnings = append(warnings, fmt.Sprintf("task missing id: %s", finding.Path))
		case missionfs.FindingUnknownTaskStatus:
			warnings = append(warnings, fmt.Sprintf("task status outside board config: %q", finding.Actual))
		case missionfs.FindingDuplicateTaskID:
			warnings = append(warnings, fmt.Sprintf("duplicate task ID %s: %s", finding.Actual, strings.Join(finding.Paths, ", ")))
		default:
			if finding.Kind == "missing_artifacts" {
				warnings = append(warnings, "artifacts missing: artifacts/")
			}
		}
	}
	return warnings
}

func missionHasStaleGit(d deps, result resolve.Result) (bool, error) {
	repo := d.missionsRepo
	if repo == "" {
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
