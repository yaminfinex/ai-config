package main

import (
	"bytes"
	"errors"
	"html/template"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type missionArtifact struct {
	Path string
	URL  string
}

type fileRefusal struct {
	kind   string
	reason string
	status int
}

func (e *fileRefusal) Error() string { return e.reason }

func artifactURL(slug, rel string) string {
	parts := strings.Split(rel, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return "/mission/" + url.PathEscape(slug) + "/file/" + strings.Join(parts, "/")
}

// missionFilePath resolves a viewer path beneath the canonical mission root.
// It checks both the lexical path and the symlink-expanded path: neither may
// leave the mission, and only mission.md or artifacts/ are exposed.
func missionFilePath(missionDir, rel string) (string, error) {
	if rel == "" || strings.IndexByte(rel, 0) >= 0 || filepath.IsAbs(rel) || path.IsAbs(rel) {
		return "", &fileRefusal{kind: "path_escape", reason: "absolute and empty file paths are refused", status: 400}
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", &fileRefusal{kind: "path_escape", reason: "file path escapes the mission folder", status: 400}
	}
	if clean != "mission.md" && !strings.HasPrefix(clean, "artifacts/") {
		return "", &fileRefusal{kind: "file_out_of_scope", reason: "only mission.md and files under artifacts/ are readable", status: 400}
	}

	root, err := filepath.Abs(missionDir)
	if err != nil {
		return "", &fileRefusal{kind: "mission_unreadable", reason: "mission folder cannot be resolved", status: 200}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", &fileRefusal{kind: "mission_unreadable", reason: "mission folder is missing or unreadable", status: 200}
	}
	candidate := filepath.Join(root, filepath.FromSlash(clean))
	inside, err := filepath.Rel(root, candidate)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", &fileRefusal{kind: "path_escape", reason: "file path escapes the mission folder", status: 400}
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", &fileRefusal{kind: "file_not_found", reason: "mission file was not found", status: 404}
		}
		return "", &fileRefusal{kind: "file_unreadable", reason: "mission file is unreadable", status: 200}
	}
	realInside, err := filepath.Rel(realRoot, realCandidate)
	if err != nil || realInside == ".." || strings.HasPrefix(realInside, ".."+string(filepath.Separator)) {
		return "", &fileRefusal{kind: "path_escape", reason: "symlink target escapes the mission folder", status: 400}
	}
	info, err := os.Stat(realCandidate)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", &fileRefusal{kind: "file_not_found", reason: "mission file was not found", status: 404}
		}
		return "", &fileRefusal{kind: "file_unreadable", reason: "mission file is unreadable", status: 200}
	}
	if !info.Mode().IsRegular() {
		return "", &fileRefusal{kind: "file_not_regular", reason: "mission file is not a regular file", status: 400}
	}
	return realCandidate, nil
}

func listMissionArtifacts(status missionStatus) ([]missionArtifact, string) {
	if !status.OK || status.MissionDir == "" {
		return nil, "mission_unreadable: artifacts unavailable until mission status recovers"
	}
	paths := []string{"mission.md"}
	artifactsDir := filepath.Join(status.MissionDir, "artifacts")
	if err := filepath.WalkDir(artifactsDir, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(status.MissionDir, name)
		if err == nil {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, "mission_unreadable: artifact listing is unavailable"
	}

	files := make([]missionArtifact, 0, len(paths))
	for _, rel := range paths {
		if _, err := missionFilePath(status.MissionDir, rel); err == nil {
			files = append(files, missionArtifact{Path: rel, URL: artifactURL(status.Slug, rel)})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, ""
}

func renderMarkdown(source []byte) (template.HTML, error) {
	var out bytes.Buffer
	markdown := goldmark.New(goldmark.WithParserOptions(parser.WithASTTransformers(
		util.Prioritized(linkSchemeFilter{}, 100),
	)))
	if err := markdown.Convert(source, &out); err != nil {
		return "", err
	}
	return template.HTML(out.String()), nil // goldmark omits raw HTML by default.
}

type linkSchemeFilter struct{}

func (linkSchemeFilter) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var refusedAutoLinks []*ast.AutoLink
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch link := node.(type) {
		case *ast.Link:
			if !allowedLinkDestination(link.Destination) {
				link.Destination = nil
			}
		case *ast.AutoLink:
			if !allowedLinkDestination(link.URL(source)) {
				refusedAutoLinks = append(refusedAutoLinks, link)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, link := range refusedAutoLinks {
		parent := link.Parent()
		parent.ReplaceChild(parent, link, ast.NewString(link.Label(source)))
	}
}

func allowedLinkDestination(destination []byte) bool {
	parsed, err := url.Parse(string(destination))
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "", "http", "https", "mailto":
		return true
	default:
		return false
	}
}
