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
			if all && cmd.Flags().Changed("mission") {
				return usageError{err: fmt.Errorf("mish status: --mission and --all are mutually exclusive — choose one status mode")}
			}
			if all {
				return runStatusOverview(cmd, d)
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
			if refusal.Kind == resolve.RefusalNoContext && isInsideMissionsRepo(cwd, d.missionsRepo) {
				return runStatusOverview(cmd, d)
			}
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

func runStatusOverview(cmd *cobra.Command, d deps) error {
	repo := d.missionsRepo
	if repo == "" {
		return refusalError{
			verb:    "status",
			message: "$MISSIONS_REPO is not set",
			remedy:  "set MISSIONS_REPO to the shared missions repo",
		}
	}
	report, err := buildStatusOverview(d, repo)
	if err != nil {
		return refusalError{
			verb:    "status",
			message: "could not read mission overview",
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

type statusOverviewRow struct {
	Slug        string
	Status      string
	Authority   string
	Owner       string
	Tasks       string
	Updated     string
	Warning     string
	StatusOrder string
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
		total := totalTasks(report.Counts)
		fmt.Fprintf(&b, "board:   %s   (%d %s)\n", formatCounts(report.Counts), total, taskWord(total))
	} else {
		fmt.Fprintf(&b, "board:   %s\n", formatCounts(report.Counts))
	}
	fmt.Fprintf(&b, "artifacts: %s\n", formatArtifacts(report.Artifacts, d.clock()))
	for _, warning := range report.Warnings {
		fmt.Fprintf(&b, "warning: %s\n", warning)
	}
	return b.String(), nil
}

func buildStatusOverview(d deps, repo string) (string, error) {
	missionsDir := filepath.Join(repo, "missions")
	entries, err := os.ReadDir(missionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			entries = nil
		} else {
			return "", err
		}
	}
	rows := make([]statusOverviewRow, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		slug := entry.Name()
		missionDir := filepath.Join(missionsDir, slug)
		row := collectStatusOverviewRow(d, resolve.Result{
			Slug:       slug,
			MissionDir: missionDir,
			Source:     resolve.SourceFlag,
		})
		rows = append(rows, row)
	}

	sharedOrder := sharedStatusOrder(rows)
	headers := []string{"SLUG", "STATUS", "AUTHORITY", "OWNER", "TASKS", "UPDATED"}
	if sharedOrder != "" {
		headers[4] = "TASKS " + sharedOrder
	}

	rendered := make([]statusOverviewRow, 0, len(rows))
	rendered = append(rendered, rows...)
	if sharedOrder == "" {
		for i := range rendered {
			if rendered[i].StatusOrder != "" {
				rendered[i].Tasks = rendered[i].Tasks + " " + rendered[i].StatusOrder
			}
		}
	}
	widths := overviewWidths(headers, rendered)

	var b strings.Builder
	fmt.Fprintf(&b, "%-*s %-*s %-*s %-*s %-*s %s\n",
		widths[0], headers[0],
		widths[1], headers[1],
		widths[2], headers[2],
		widths[3], headers[3],
		widths[4], headers[4],
		headers[5],
	)
	for _, row := range rendered {
		fmt.Fprintf(&b, "%-*s %-*s %-*s %-*s %-*s %s",
			widths[0], row.Slug,
			widths[1], row.Status,
			widths[2], row.Authority,
			widths[3], row.Owner,
			widths[4], row.Tasks,
			row.Updated,
		)
		if row.Warning != "" {
			fmt.Fprintf(&b, "  warning: %s", row.Warning)
		}
		fmt.Fprintln(&b)
	}
	return b.String(), nil
}

func collectStatusOverviewRow(d deps, result resolve.Result) statusOverviewRow {
	row := statusOverviewRow{
		Slug:      result.Slug,
		Status:    "warning",
		Authority: "unknown",
		Owner:     "unknown",
		Tasks:     "unavailable",
		Updated:   updatedAgo(d.clock(), newestMissionMTime(result.MissionDir)),
	}

	var findings []missionfs.Finding
	manifest, manifestFindings, err := missionfs.ReadManifest(result.MissionDir)
	if err != nil {
		findings = append(findings, missionfs.Finding{
			Kind:   missionfs.FindingMalformedManifest,
			Path:   filepath.Join(result.MissionDir, "mission.md"),
			Actual: err.Error(),
		})
	} else {
		row.Status = valueOrUnknown(manifest.Status)
		row.Authority = valueOrUnknown(manifest.Authority)
		row.Owner = valueOrUnknown(manifest.Owner)
		findings = append(findings, manifestFindings...)
	}

	boardDir := filepath.Join(result.MissionDir, "backlog")
	cfg, boardFindings, err := missionfs.ReadBoardConfig(boardDir)
	if err != nil {
		findings = append(findings, missionfs.Finding{
			Kind:   missionfs.FindingMalformedBoardConfig,
			Path:   filepath.Join(boardDir, "config.yml"),
			Actual: err.Error(),
		})
	} else {
		findings = append(findings, boardFindings...)
		if !cfg.Missing && len(cfg.Statuses) > 0 {
			taskScan, err := missionfs.ScanTasks(boardDir)
			if err != nil {
				findings = append(findings, missionfs.Finding{
					Kind:   missionfs.FindingMalformedTask,
					Path:   boardDir,
					Actual: err.Error(),
				})
			} else {
				orderedCounts, orderedFindings := taskScan.OrderedCounts(cfg.Statuses)
				row.Tasks = formatSlashCounts(orderedCounts)
				row.StatusOrder = formatStatusHeader(orderedCounts)
				findings = append(findings, taskScan.Findings...)
				findings = append(findings, orderedFindings...)
			}
		}
	}

	if warnings := formatFindings(result.MissionDir, findings); len(warnings) > 0 {
		row.Warning = warnings[0]
	}
	return row
}

func sharedStatusOrder(rows []statusOverviewRow) string {
	if len(rows) == 0 || rows[0].StatusOrder == "" {
		return ""
	}
	order := rows[0].StatusOrder
	for _, row := range rows[1:] {
		if row.StatusOrder != order {
			return ""
		}
	}
	return order
}

func overviewWidths(headers []string, rows []statusOverviewRow) []int {
	widths := []int{
		len(headers[0]),
		len(headers[1]),
		len(headers[2]),
		len(headers[3]),
		len(headers[4]),
		len(headers[5]),
	}
	for _, row := range rows {
		values := []string{row.Slug, row.Status, row.Authority, row.Owner, row.Tasks, row.Updated}
		for i, value := range values {
			if len(value) > widths[i] {
				widths[i] = len(value)
			}
		}
	}
	return widths
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

func formatSlashCounts(counts []missionfs.StatusCount) string {
	if len(counts) == 0 {
		return "unavailable"
	}
	parts := make([]string, 0, len(counts))
	for _, count := range counts {
		parts = append(parts, strconv.Itoa(count.Count))
	}
	return strings.Join(parts, "/")
}

func formatStatusHeader(counts []missionfs.StatusCount) string {
	statuses := make([]string, 0, len(counts))
	for _, count := range counts {
		statuses = append(statuses, count.Status)
	}
	return strings.Join(statuses, "/")
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

func taskWord(total int) string {
	if total == 1 {
		return "task"
	}
	return "tasks"
}

func updatedAgo(now, then time.Time) string {
	value := ago(now, then)
	if value == "unknown" {
		return value
	}
	return value + " ago"
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

func newestMissionMTime(missionDir string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(missionDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}

func isInsideMissionsRepo(cwd, repo string) bool {
	if cwd == "" || repo == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(repo), filepath.Clean(cwd))
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
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
