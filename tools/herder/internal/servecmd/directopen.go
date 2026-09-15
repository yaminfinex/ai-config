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
// directory without consulting any index. Inside a git repository the root
// is the innermost top level and the path is relative to it; outside any
// repository the root is the parent directory and the path is the base name.
// A path that is itself a repository top level answers root = top, path = "". A path that does not
// exist, or that fileapi would refuse (escaping symlink, .git internals,
// non-regular), is reported as not handled.
func directOpen(ctx context.Context, query string) (resolveResponse, bool) {
	path := filepath.Clean(query)
	if !filepath.IsAbs(path) || path == string(filepath.Separator) {
		return resolveResponse{}, false
	}
	for _, component := range strings.Split(path, string(filepath.Separator)) {
		if component == ".git" {
			return resolveResponse{}, false
		}
	}
	if top, ok, err := repoctx.TopLevel(ctx, path); err == nil && ok && top == path {
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			return resolveResponse{
				Candidates: []fileresolver.Result{{Root: path, Path: "", Kind: filecandidate.KindDir, Tier: fileresolver.TierExact}},
				Roots:      []fileresolver.RootOutcome{{Root: path, Status: fileresolver.RootComplete}},
			}, true
		}
	}
	parent := filepath.Dir(path)
	root, relative := parent, filepath.Base(path)
	if top, ok, err := repoctx.TopLevel(ctx, parent); err == nil && ok {
		if rel, relErr := filepath.Rel(top, path); relErr == nil {
			root, relative = top, rel
		}
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

// directOpenRoot reports whether root is acceptable to the file endpoints
// outside the live set: an absolute, clean, existing directory, which is the
// root shape directOpen emits.
func directOpenRoot(root string) bool {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return false
	}
	info, err := os.Stat(root)
	return err == nil && info.IsDir()
}
