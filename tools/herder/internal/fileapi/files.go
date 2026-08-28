// Package fileapi implements bounded, root-contained file reads and trees.
package fileapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"ai-config/tools/herder/internal/fileresolver"
)

const (
	SoftCap int64 = 256 * 1024
	HardCap int64 = 4 * 1024 * 1024
)

var (
	ErrNotFound = errors.New("file path not found")
	ErrRefused  = errors.New("file path refused")
)

type File struct {
	Root      string    `json:"root"`
	Path      string    `json:"path"`
	Content   *string   `json:"content,omitempty"`
	Binary    bool      `json:"binary"`
	Size      int64     `json:"size"`
	Truncated *bool     `json:"truncated,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

type TreeResult struct {
	Root    string  `json:"root"`
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

type Entry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Size *int64 `json:"size,omitempty"`
}

func Read(root, path string, now func() time.Time) (File, error) {
	relative, err := validateRelative(path, false)
	if err != nil {
		return File{}, err
	}
	resolved, err := resolve(root, relative)
	if err != nil {
		return File{}, err
	}
	if isGitInternal(root, resolved) {
		return File{}, fmt.Errorf("%w: .git internals are not served: %q resolves to %q", ErrRefused, filepath.Join(root, relative), resolved)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return File{}, classifyPathError(root, path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return File{}, fmt.Errorf("stat file %q: %w", resolved, err)
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("%w: path %q resolves to non-file %q", ErrRefused, filepath.Join(root, relative), resolved)
	}
	if info.Size() > HardCap {
		return File{}, fmt.Errorf("%w: file %q is %d bytes; files above 4 MiB are not served", ErrRefused, resolved, info.Size())
	}
	content, err := io.ReadAll(io.LimitReader(file, SoftCap+1))
	if err != nil {
		return File{}, fmt.Errorf("read file %q: %w", resolved, err)
	}
	result := File{Root: root, Path: filepath.ToSlash(relative), Size: info.Size(), FetchedAt: now()}
	if bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
		result.Binary = true
		return result, nil
	}
	truncated := int64(len(content)) > SoftCap
	if truncated {
		content = content[:SoftCap]
		for len(content) > 0 && !utf8.Valid(content) {
			content = content[:len(content)-1]
		}
	}
	text := string(content)
	result.Content = &text
	result.Truncated = &truncated
	return result, nil
}

func Tree(root, path string) (TreeResult, error) {
	relative, err := validateRelative(path, true)
	if err != nil {
		return TreeResult{}, err
	}
	resolved, err := resolve(root, relative)
	if err != nil {
		return TreeResult{}, err
	}
	if isGitInternal(root, resolved) {
		return TreeResult{}, fmt.Errorf("%w: .git internals are not served: %q resolves to %q", ErrRefused, filepath.Join(root, relative), resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return TreeResult{}, classifyPathError(root, path, err)
	}
	if !info.IsDir() {
		return TreeResult{}, fmt.Errorf("%w: path %q resolves to non-directory %q", ErrRefused, filepath.Join(root, relative), resolved)
	}
	children, err := os.ReadDir(resolved)
	if err != nil {
		return TreeResult{}, fmt.Errorf("read directory %q: %w", resolved, err)
	}
	entries := make([]Entry, 0, len(children))
	for _, child := range children {
		if child.Name() == ".git" {
			continue
		}
		entry := Entry{Name: child.Name()}
		switch child.Type() & os.ModeType {
		case os.ModeSymlink:
			entry.Kind = "symlink"
		case os.ModeDir:
			entry.Kind = "directory"
		default:
			entry.Kind = "file"
			childInfo, infoErr := child.Info()
			if infoErr != nil {
				return TreeResult{}, fmt.Errorf("stat directory entry %q: %w", filepath.Join(resolved, child.Name()), infoErr)
			}
			size := childInfo.Size()
			entry.Size = &size
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	publicPath := ""
	if relative != "." {
		publicPath = filepath.ToSlash(relative)
	}
	return TreeResult{Root: root, Path: publicPath, Entries: entries}, nil
}

func validateRelative(path string, allowEmpty bool) (string, error) {
	if strings.IndexByte(path, 0) >= 0 || filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: path must be root-relative: %q", ErrRefused, path)
	}
	if path == "" && allowEmpty {
		return ".", nil
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if cleaned == "." && !allowEmpty {
		return "", fmt.Errorf("%w: file path must not be empty", ErrRefused)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path escapes root: %q", ErrRefused, path)
	}
	for _, component := range strings.Split(cleaned, string(filepath.Separator)) {
		if component == ".git" {
			return "", fmt.Errorf("%w: .git internals are not served: %q", ErrRefused, path)
		}
	}
	return cleaned, nil
}

func resolve(root, relative string) (string, error) {
	resolved, err := fileresolver.ResolveWithinRoot(root, relative)
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return "", fmt.Errorf("%w: %v", ErrRefused, err)
}

func classifyPathError(root, path string, err error) error {
	if os.IsNotExist(err) {
		return fmt.Errorf("%w: path %q under root %q: %v", ErrNotFound, path, root, err)
	}
	return fmt.Errorf("access path %q under root %q: %w", path, root, err)
}

func isGitInternal(root, resolved string) bool {
	gitPath := filepath.Join(filepath.Clean(root), ".git")
	relative, err := filepath.Rel(gitPath, resolved)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
