package servecmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"ai-config/tools/herder/internal/fileapi"
	"ai-config/tools/herder/internal/filecandidate"
	"ai-config/tools/herder/internal/fileresolver"
	"ai-config/tools/herder/internal/repoctx"
)

// directOpen answers an absolute query that names an existing file or
// directory without consulting any index. The root is chosen from the
// LEXICAL path, never from a followed symlink: the longest lexical prefix
// below the first symlink component anchors the lookup. If that anchor sits in a
// git repository the root is the innermost top level and the path is the
// remainder (empty when the query is the top level itself); otherwise the
// root is the anchor and the path is the remainder below it. fileapi.Stat
// then validates containment through any symlink in the remainder, so an
// escaping link or an alias into .git is refused and reported as not handled,
// exactly like a path that does not exist.
func directOpen(ctx context.Context, query string) (resolveResponse, bool) {
	path := filepath.Clean(query)
	if !filepath.IsAbs(path) || path == string(filepath.Separator) || hasGitComponent(path) {
		return resolveResponse{}, false
	}
	anchor := lexicalAnchor(filepath.Dir(path))
	if anchor == "" {
		return resolveResponse{}, false
	}
	root := anchor
	if top, ok := repoctx.TopLevel(ctx, anchor); ok {
		root = top
	}
	// The query itself, when it is a real directory that is a git top level,
	// is its own root with an empty path.
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		if top, ok := repoctx.TopLevel(ctx, path); ok && top == path {
			root = path
		}
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return resolveResponse{}, false
	}
	if relative == "." {
		relative = ""
	}
	kind, err := fileapi.Stat(root, relative)
	if err != nil {
		return resolveResponse{}, false
	}
	candidateKind := filecandidate.KindFile
	if kind == "directory" {
		candidateKind = filecandidate.KindDir
	}
	return resolveResponse{
		Candidates: []fileresolver.Result{{Root: root, Path: filepath.ToSlash(relative), Kind: candidateKind, Tier: fileresolver.TierExact}},
		Roots:      []fileresolver.RootOutcome{{Root: root, Status: fileresolver.RootComplete}},
	}, true
}

// lexicalAnchor walks the lexical prefixes of dir from the filesystem root
// DOWN and returns the last prefix before the first symlink component, so
// every ancestor of the anchor is a real directory. A missing prefix stops the
// walk at its parent; a prefix that is a regular file yields "".
func lexicalAnchor(dir string) string {
	anchor := string(filepath.Separator)
	prefix := anchor
	for _, component := range strings.Split(strings.TrimPrefix(dir, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		prefix = filepath.Join(prefix, component)
		info, err := os.Lstat(prefix)
		switch {
		case err != nil:
			return anchor
		case info.Mode()&os.ModeSymlink != 0:
			return anchor
		case !info.IsDir():
			return ""
		}
		anchor = prefix
	}
	return anchor
}

// directOpenRoot reports whether root is acceptable to the file endpoints
// outside the live set: an absolute, clean, existing directory that holds no
// .git component either lexically or at its resolved location. This is the
// root shape directOpen emits.
func directOpenRoot(root string) bool {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || hasGitComponent(root) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || hasGitComponent(resolved) {
		return false
	}
	info, err := os.Stat(root)
	return err == nil && info.IsDir()
}

// hasGitComponent reports whether any path component is .git.
func hasGitComponent(path string) bool {
	for _, component := range strings.Split(path, string(filepath.Separator)) {
		if component == ".git" {
			return true
		}
	}
	return false
}
