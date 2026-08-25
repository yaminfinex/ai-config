// Package webui exposes the committed production UI embedded in the herder
// binary. Regenerating dist requires Node; compiling and running herder does not.
package webui

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

//go:embed dist
var assets embed.FS

// Files returns the production UI rooted at dist.
func Files() fs.FS {
	files, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return files
}

// Has reports whether a request path names an embedded UI asset.
func Has(requestPath string) bool {
	name := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	if name == "" || strings.HasSuffix(requestPath, "/") {
		name = path.Join(name, "index.html")
	}
	info, err := fs.Stat(Files(), name)
	return err == nil && !info.IsDir()
}
