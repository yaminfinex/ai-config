// Package backlogapi reads the bounded board facts exposed by /api/backlog.
package backlogapi

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ai-config/tools/herder/internal/fileapi"
	"ai-config/tools/herder/internal/fileresolver"

	"gopkg.in/yaml.v3"
)

const (
	FrontmatterCap   int64 = 64 * 1024
	TaskCap                = 2000
	frontmatterOpen        = "---\n"
	frontmatterClose       = "\n---\n"
)

// Unavailable explains why a readable directory is not a Backlog.md board.
type Unavailable struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// Task is the closed task-frontmatter projection served by the board endpoint.
type Task struct {
	ID          string    `json:"id,omitempty" yaml:"id"`
	Title       string    `json:"title,omitempty" yaml:"title"`
	Status      string    `json:"status,omitempty" yaml:"status"`
	Ordinal     *int      `json:"ordinal,omitempty" yaml:"ordinal"`
	Labels      *[]string `json:"labels,omitempty" yaml:"labels"`
	Priority    string    `json:"priority,omitempty" yaml:"priority"`
	Assignee    *[]string `json:"assignee,omitempty" yaml:"assignee"`
	CreatedDate string    `json:"created_date,omitempty" yaml:"created_date"`
	UpdatedDate string    `json:"updated_date,omitempty" yaml:"updated_date"`
	File        string    `json:"file" yaml:"-"`
}

// Unparsed quarantines one selected task file without hiding it.
type Unparsed struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// Result is either the board facts or a 200-unavailable explanation.
type Result struct {
	Root      string       `json:"root"`
	Path      string       `json:"path"`
	Backlog   *Unavailable `json:"backlog,omitempty"`
	Statuses  *[]string    `json:"statuses,omitempty"`
	Tasks     *[]Task      `json:"tasks,omitempty"`
	Unparsed  *[]Unparsed  `json:"unparsed,omitempty"`
	Truncated *bool        `json:"truncated,omitempty"`
	FetchedAt time.Time    `json:"fetched_at"`
}

type boardConfig struct {
	Statuses []string `yaml:"statuses"`
}

// Read returns current board facts. Missing Backlog.md markers are an honest
// unavailable result; containment and filesystem errors retain fileapi errors.
func Read(root, requestedPath string, now func() time.Time) (Result, error) {
	tree, err := fileapi.Tree(root, requestedPath)
	if err != nil {
		return Result{}, err
	}
	result := Result{Root: root, Path: tree.Path, FetchedAt: now()}

	entries := make(map[string]string, len(tree.Entries))
	for _, entry := range tree.Entries {
		entries[entry.Name] = entry.Kind
	}
	if _, ok := entries["config.yml"]; !ok {
		return unavailable(result, "directory does not contain config.yml"), nil
	}
	if _, ok := entries["tasks"]; !ok {
		return unavailable(result, "directory does not contain tasks/"), nil
	}

	configPath := joinRelative(tree.Path, "config.yml")
	configFile, err := fileapi.Read(root, configPath, now)
	if err != nil {
		return Result{}, err
	}
	if configFile.Binary || configFile.Content == nil {
		return unavailable(result, "config.yml is not UTF-8 text"), nil
	}
	if configFile.Size > FrontmatterCap {
		return unavailable(result, fmt.Sprintf("config.yml exceeds %d-byte cap", FrontmatterCap)), nil
	}
	var config boardConfig
	if err := yaml.Unmarshal([]byte(*configFile.Content), &config); err != nil {
		return unavailable(result, fmt.Sprintf("config.yml cannot be parsed: %v", err)), nil
	}
	if len(config.Statuses) == 0 {
		return unavailable(result, "config.yml does not define statuses"), nil
	}

	tasksPath := joinRelative(tree.Path, "tasks")
	taskTree, err := fileapi.Tree(root, tasksPath)
	if err != nil {
		return Result{}, err
	}
	files := make([]string, 0, len(taskTree.Entries))
	for _, entry := range taskTree.Entries {
		if entry.Kind == "file" && strings.EqualFold(filepath.Ext(entry.Name), ".md") {
			files = append(files, entry.Name)
		}
	}
	sort.Strings(files)
	truncated := len(files) > TaskCap
	if truncated {
		files = files[:TaskCap]
	}
	statuses := append([]string(nil), config.Statuses...)
	tasks := make([]Task, 0, len(files))
	unparsed := make([]Unparsed, 0)
	for _, name := range files {
		relative := joinRelative(tasksPath, name)
		publicFile := filepath.ToSlash(filepath.Join("tasks", name))
		task, err := readTask(root, relative)
		if err != nil {
			unparsed = append(unparsed, Unparsed{File: publicFile, Reason: err.Error()})
			continue
		}
		task.File = publicFile
		tasks = append(tasks, task)
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		left, right := tasks[i], tasks[j]
		if left.Ordinal == nil && right.Ordinal != nil {
			return false
		}
		if left.Ordinal != nil && right.Ordinal == nil {
			return true
		}
		if left.Ordinal != nil && right.Ordinal != nil && *left.Ordinal != *right.Ordinal {
			return *left.Ordinal < *right.Ordinal
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return left.File < right.File
	})
	result.Statuses = &statuses
	result.Tasks = &tasks
	result.Unparsed = &unparsed
	result.Truncated = &truncated
	return result, nil
}

func unavailable(result Result, reason string) Result {
	result.Backlog = &Unavailable{Status: "unavailable", Reason: reason}
	return result
}

func readTask(root, relative string) (Task, error) {
	resolved, err := fileresolver.ResolveWithinRoot(root, filepath.FromSlash(relative))
	if err != nil {
		return Task{}, err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return Task{}, fmt.Errorf("open task frontmatter: %w", err)
	}
	defer file.Close()
	probeCap := FrontmatterCap + int64(len(frontmatterOpen)+len(frontmatterClose))
	data, err := io.ReadAll(io.LimitReader(file, probeCap+1))
	if err != nil {
		return Task{}, fmt.Errorf("read task frontmatter: %w", err)
	}
	frontmatter, err := splitFrontmatter(data)
	if err != nil {
		return Task{}, err
	}
	var task Task
	decoder := yaml.NewDecoder(bytes.NewReader(frontmatter))
	if err := decoder.Decode(&task); err != nil {
		return Task{}, fmt.Errorf("parse task frontmatter: %w", err)
	}
	return task, nil
}

func splitFrontmatter(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte(frontmatterOpen)) {
		return nil, fmt.Errorf("task is missing opening YAML frontmatter delimiter")
	}
	rest := data[len(frontmatterOpen):]
	if end := bytes.Index(rest, []byte(frontmatterClose)); end >= 0 {
		if int64(end) > FrontmatterCap {
			return nil, fmt.Errorf("task frontmatter exceeds %d-byte cap", FrontmatterCap)
		}
		return rest[:end], nil
	}
	if bytes.HasSuffix(rest, []byte("\n---")) {
		end := len(rest) - len("\n---")
		if int64(end) > FrontmatterCap {
			return nil, fmt.Errorf("task frontmatter exceeds %d-byte cap", FrontmatterCap)
		}
		return rest[:end], nil
	}
	if int64(len(rest)) > FrontmatterCap {
		return nil, fmt.Errorf("task frontmatter exceeds %d-byte cap", FrontmatterCap)
	}
	return nil, fmt.Errorf("task YAML frontmatter is not closed")
}

func joinRelative(base, name string) string {
	if base == "" {
		return name
	}
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(base), name))
}
