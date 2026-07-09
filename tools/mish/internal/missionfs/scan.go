package missionfs

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StatusCount is a count in the board's configured status order.
type StatusCount struct {
	Status string
	Count  int
}

// TaskScan summarizes task frontmatter without invoking Backlog.md.
type TaskScan struct {
	Counts      map[string]int
	StatusPaths map[string][]string
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
		return nil
	})
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

type taskFrontmatter struct {
	ID     string
	Status string
}

func readTaskFrontmatter(path string) (taskFrontmatter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return taskFrontmatter{}, err
	}
	frontmatter, err := splitFrontmatter(data)
	if err != nil {
		return taskFrontmatter{}, fmt.Errorf("%s: %w", path, err)
	}
	var task taskFrontmatter
	scanner := bufio.NewScanner(bytes.NewReader(frontmatter))
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "id":
			task.ID = value
		case "status":
			task.Status = value
		}
	}
	if err := scanner.Err(); err != nil {
		return taskFrontmatter{}, err
	}
	return task, nil
}

// ArtifactScan summarizes artifacts without interpreting their contents.
type ArtifactScan struct {
	Missing    bool
	Count      int
	NewestTime time.Time
}

// ScanArtifacts counts files under artifactsDir and records the newest file mtime.
func ScanArtifacts(artifactsDir string) (ArtifactScan, error) {
	info, err := os.Stat(artifactsDir)
	if os.IsNotExist(err) {
		return ArtifactScan{Missing: true}, nil
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
			scan.NewestTime = info.ModTime()
		}
		return nil
	})
	return scan, err
}
