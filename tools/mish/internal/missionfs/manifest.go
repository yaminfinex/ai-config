package missionfs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const manifestName = "mission.md"

var manifestKeys = []string{"mission", "authority", "owner", "status", "created"}

// Manifest is the normative mission.md frontmatter.
type Manifest struct {
	Mission   string `yaml:"mission"`
	Authority string `yaml:"authority"`
	Owner     string `yaml:"owner"`
	Status    string `yaml:"status"`
	Created   string `yaml:"created"`
}

// FindingKind names a machine-readable status finding.
type FindingKind string

const (
	FindingUnknownManifestKey    FindingKind = "unknown_manifest_key"
	FindingMissingManifestKey    FindingKind = "missing_manifest_key"
	FindingManifestSlugMismatch  FindingKind = "manifest_slug_mismatch"
	FindingInvalidManifestStatus FindingKind = "invalid_manifest_status"
	FindingMissingBoard          FindingKind = "missing_board"
	FindingBoardPinDrift         FindingKind = "board_pin_drift"
	FindingMalformedTask         FindingKind = "malformed_task"
	FindingMissingTaskID         FindingKind = "missing_task_id"
	FindingUnknownTaskStatus     FindingKind = "unknown_task_status"
	FindingDuplicateTaskID       FindingKind = "duplicate_task_id"
)

// Finding is returned by read-only scans for later CLI presentation.
type Finding struct {
	Kind     FindingKind
	Key      string
	Expected string
	Actual   string
	Path     string
	Paths    []string
}

// ReadManifest reads mission.md from a mission directory and validates the closed
// frontmatter surface that status warns about.
func ReadManifest(missionDir string) (Manifest, []Finding, error) {
	path := filepath.Join(missionDir, manifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, nil, err
	}
	frontmatter, err := splitFrontmatter(data)
	if err != nil {
		return Manifest{}, nil, err
	}

	var node yaml.Node
	if err := yaml.Unmarshal(frontmatter, &node); err != nil {
		return Manifest{}, nil, fmt.Errorf("parse manifest frontmatter: %w", err)
	}

	var manifest Manifest
	if err := node.Decode(&manifest); err != nil {
		return Manifest{}, nil, fmt.Errorf("decode manifest frontmatter: %w", err)
	}

	findings := manifestKeyFindings(&node)
	dirSlug := filepath.Base(missionDir)
	if manifest.Mission != "" && manifest.Mission != dirSlug {
		findings = append(findings, Finding{
			Kind:     FindingManifestSlugMismatch,
			Key:      "mission",
			Expected: dirSlug,
			Actual:   manifest.Mission,
			Path:     path,
		})
	}
	if manifest.Status != "" && manifest.Status != "active" && manifest.Status != "closed" {
		findings = append(findings, Finding{
			Kind:     FindingInvalidManifestStatus,
			Key:      "status",
			Expected: "active|closed",
			Actual:   manifest.Status,
			Path:     path,
		})
	}
	return manifest, findings, nil
}

// WriteManifest writes the scaffolded §4.2 skeleton exactly.
func WriteManifest(path string, manifest Manifest, title string) error {
	var b bytes.Buffer
	fmt.Fprintln(&b, "---")
	fmt.Fprintf(&b, "mission: %s\n", manifest.Mission)
	fmt.Fprintf(&b, "authority: %s\n", manifest.Authority)
	fmt.Fprintf(&b, "owner: %s\n", manifest.Owner)
	fmt.Fprintf(&b, "status: %s\n", manifest.Status)
	fmt.Fprintf(&b, "created: %s\n", manifest.Created)
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "# %s\n", title)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Purpose")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Scope")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Decisions")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Closeout")
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func splitFrontmatter(data []byte) ([]byte, error) {
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("manifest missing YAML frontmatter")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, fmt.Errorf("manifest frontmatter is not closed")
	}
	return []byte(rest[:end]), nil
}

func manifestKeyFindings(node *yaml.Node) []Finding {
	mapping := firstMapping(node)
	if mapping == nil {
		return nil
	}
	seen := map[string]bool{}
	allowed := map[string]bool{}
	for _, key := range manifestKeys {
		allowed[key] = true
	}
	var findings []Finding
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i].Value
		seen[key] = true
		if !allowed[key] {
			findings = append(findings, Finding{Kind: FindingUnknownManifestKey, Key: key})
		}
	}
	for _, key := range manifestKeys {
		if !seen[key] {
			findings = append(findings, Finding{Kind: FindingMissingManifestKey, Key: key})
		}
	}
	return findings
}

func firstMapping(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}
