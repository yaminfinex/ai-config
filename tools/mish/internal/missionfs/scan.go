package missionfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// StatusCount is a count in the board's configured status order.
type StatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// Task is the agent-facing subset of Backlog.md task frontmatter.
type Task struct {
	ID      string   `json:"id" yaml:"id"`
	Title   string   `json:"title" yaml:"title"`
	Status  string   `json:"status" yaml:"status"`
	Ordinal int      `json:"ordinal" yaml:"ordinal"`
	Labels  []string `json:"labels" yaml:"labels"`
}

// TaskScan summarizes task frontmatter without invoking Backlog.md.
type TaskScan struct {
	Counts      map[string]int
	StatusPaths map[string][]string
	Tasks       []Task
	Findings    []Finding
}

// OrderedCounts returns counts in the board config's status order and reports
// task statuses outside that configured vocabulary.
func (s TaskScan) OrderedCounts(statuses []string) ([]StatusCount, []Finding) {
	out := make([]StatusCount, 0, len(statuses))
	known := map[string]bool{}
	for _, status := range statuses {
		known[status] = true
		out = append(out, StatusCount{Status: status, Count: s.Counts[status]})
	}
	var findings []Finding
	for status := range s.Counts {
		if !known[status] {
			findings = append(findings, Finding{
				Kind:     FindingUnknownTaskStatus,
				Key:      "status",
				Actual:   status,
				Expected: strings.Join(statuses, "|"),
				Paths:    s.StatusPaths[status],
			})
		}
	}
	return out, findings
}

// ScanTasks reads task frontmatter from backlog/tasks and backlog/completed only.
func ScanTasks(boardDir string) (TaskScan, error) {
	scan := TaskScan{
		Counts:      map[string]int{},
		StatusPaths: map[string][]string{},
	}
	seen := map[string][]string{}
	for _, rel := range []string{"tasks", "completed"} {
		dir := filepath.Join(boardDir, rel)
		if err := scanTaskDir(dir, &scan, seen); err != nil {
			return TaskScan{}, err
		}
	}
	for id, paths := range seen {
		if id != "" && len(paths) > 1 {
			scan.Findings = append(scan.Findings, Finding{
				Kind:   FindingDuplicateTaskID,
				Key:    "id",
				Actual: id,
				Paths:  paths,
			})
		}
	}
	sort.Slice(scan.Tasks, func(i, j int) bool {
		if scan.Tasks[i].Ordinal != scan.Tasks[j].Ordinal {
			return scan.Tasks[i].Ordinal < scan.Tasks[j].Ordinal
		}
		return scan.Tasks[i].ID < scan.Tasks[j].ID
	})
	return scan, nil
}

func scanTaskDir(dir string, scan *TaskScan, seen map[string][]string) error {
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		task, err := readTaskFrontmatter(path)
		if err != nil {
			scan.Findings = append(scan.Findings, Finding{
				Kind:   FindingMalformedTask,
				Path:   path,
				Actual: err.Error(),
			})
			return nil
		}
		if task.Status != "" {
			scan.Counts[task.Status]++
			scan.StatusPaths[task.Status] = append(scan.StatusPaths[task.Status], path)
		}
		if task.ID != "" {
			seen[task.ID] = append(seen[task.ID], path)
		} else {
			scan.Findings = append(scan.Findings, Finding{
				Kind: FindingMissingTaskID,
				Key:  "id",
				Path: path,
			})
		}
		if task.Labels == nil {
			task.Labels = []string{}
		}
		scan.Tasks = append(scan.Tasks, task)
		return nil
	})
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func readTaskFrontmatter(path string) (Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Task{}, err
	}
	frontmatter, err := splitFrontmatter(data)
	if err != nil {
		return Task{}, fmt.Errorf("%s: %w", path, err)
	}
	var task Task
	if err := yaml.Unmarshal(frontmatter, &task); err != nil {
		return Task{}, fmt.Errorf("%s: parse frontmatter: %w", path, err)
	}
	return task, nil
}

// ArtifactScan summarizes artifacts without interpreting their contents.
type ArtifactScan struct {
	Missing    bool
	Count      int
	NewestPath string
	NewestTime time.Time
	Findings   []Finding
}

// ScanArtifacts counts files under artifactsDir and records the newest file mtime.
func ScanArtifacts(artifactsDir string) (ArtifactScan, error) {
	info, err := os.Stat(artifactsDir)
	if os.IsNotExist(err) {
		return ArtifactScan{
			Missing: true,
			Findings: []Finding{{
				Kind: FindingMissingArtifacts,
				Path: artifactsDir,
			}},
		}, nil
	}
	if err != nil {
		return ArtifactScan{}, err
	}
	if !info.IsDir() {
		return ArtifactScan{}, fmt.Errorf("artifacts path is not a directory: %s", artifactsDir)
	}

	var scan ArtifactScan
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
		scan.Count++
		if info.ModTime().After(scan.NewestTime) {
			rel, err := filepath.Rel(artifactsDir, path)
			if err != nil {
				return err
			}
			scan.NewestPath = filepath.ToSlash(rel)
			scan.NewestTime = info.ModTime()
		}
		return nil
	})
	return scan, err
}
