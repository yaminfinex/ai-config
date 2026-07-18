package main

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// The React app (tools/mc/ui) ships inside the one mc binary via go:embed —
// deployment stays one binary, one port, one Tailscale header path. During
// the transition the SPA mounts under /ui/ while the legacy HTML pages keep
// serving; surfaces move over one at a time. In dev the Vite server owns the
// page and proxies /api here instead.
//
// ui/dist holds only .gitkeep until `bun run build` runs; an unbuilt tree
// serves an honest 501 rather than a broken page.

//go:embed all:ui/dist
var uiDist embed.FS

func uiHandler(fsys fs.FS) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/ui")
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			rel = "index.html"
		}
		name := path.Clean(rel)
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			if assetShaped(name) {
				http.NotFound(rw, r)
				return
			}
			// Client-side routes (/ui/mission/x) resolve to the SPA shell.
			name = "index.html"
			data, err = fs.ReadFile(fsys, name)
			if err != nil {
				http.Error(rw, "ui not built: run `bun run build` in tools/mc/ui", http.StatusNotImplemented)
				return
			}
		}
		if ctype := mime.TypeByExtension(path.Ext(name)); ctype != "" {
			rw.Header().Set("Content-Type", ctype)
		}
		if strings.HasPrefix(name, "assets/") {
			// Vite content-hashes everything under assets/.
			rw.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			rw.Header().Set("Cache-Control", "no-cache")
		}
		_, _ = rw.Write(data)
	}
}

// assetShaped reports whether a missed path names a built asset rather than
// a client-side route. A stale asset URL served as 200 HTML would masquerade
// as a working script, so asset misses get an honest 404 — but route params
// may legitimately look like file paths (artifact identities ARE paths), so
// "has an extension" is not the test. Only the reserved namespaces 404: the
// assets/ tree (Vite's hashed output) and the finite set of root files a
// build may emit. Extend the list when the build grows a public/ file.
func assetShaped(name string) bool {
	if name == "assets" || strings.HasPrefix(name, "assets/") {
		return true
	}
	switch name {
	case "favicon.ico", "robots.txt":
		return true
	}
	return false
}

func uiDistFS() fs.FS {
	sub, err := fs.Sub(uiDist, "ui/dist")
	if err != nil {
		panic(err) // embed guarantees the directory exists
	}
	return sub
}
