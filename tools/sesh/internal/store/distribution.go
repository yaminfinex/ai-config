package store

import (
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Distribution endpoints (design 2026-07-12 §3): the store serves its own
// release channel from the ingest listener, so the distribution URL IS the
// store URL. Operator surface only — no transcript data, no state mutation,
// and no shipper *shipping* behavior switches on these routes (informational
// note in sesh-wire.md; the wire contract is untouched).
//
//	GET /install.sh                     installer, {{BASE}} from the request
//	GET /releases/latest/VERSION        the ONLY latest endpoint
//	GET /releases/<ver>/sesh-<os>-<arch>  immutable
//	GET /releases/<ver>/SHA256SUMS        immutable
//
// There is deliberately no latest/<asset> route: installer and updater read
// latest/VERSION once and fetch everything else from immutable paths, so a
// latest flip mid-download cannot mix artifacts from two releases.

//go:embed assets/install.sh
var installScript string

// releasesDirName holds published releases under the store data dir:
// releases/<ver>/... plus a `latest` file carrying the version string.
// Rebuildable class for backup purposes (re-publish regenerates it).
const releasesDirName = "releases"

// ReleasesDir returns the on-disk release channel root for a data dir.
func ReleasesDir(dataDir string) string {
	return filepath.Join(dataDir, releasesDirName)
}

// IsDistributionPath reports whether an ingest-listener request path belongs
// to the distribution surface. Used for route-scoped auth: distribution
// accepts EITHER grant verb (ship or read) in tsnet mode, while everything
// else on the ingest listener stays ship-only.
func IsDistributionPath(p string) bool {
	return p == "/install.sh" || p == "/releases" || strings.HasPrefix(p, "/releases/")
}

var releaseSegmentRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func (s *Store) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	base, err := requestBase(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	script := strings.ReplaceAll(installScript, "{{BASE}}", base)
	h := w.Header()
	h.Set("Content-Type", "text/x-shellscript; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(script))
}

func (s *Store) handleReleases(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/releases/"), "/")
	if len(parts) != 2 || !releaseSegmentRE.MatchString(parts[0]) || !releaseSegmentRE.MatchString(parts[1]) {
		http.Error(w, "unknown release path", http.StatusNotFound)
		return
	}
	ver, file := parts[0], parts[1]
	// The segment charset admits dots; dot-only segments are the one
	// traversal shape it leaves open (the mux also redirects these — this
	// is defense in depth).
	if ver == "." || ver == ".." || file == "." || file == ".." {
		http.Error(w, "unknown release path", http.StatusNotFound)
		return
	}
	if ver == "latest" {
		// The pointer flip is atomic+durable on the publish side
		// (temp+rename+dir-fsync); reads never see a torn value.
		if file != "VERSION" {
			http.Error(w, "latest serves only VERSION; fetch assets from immutable /releases/<version>/ paths", http.StatusNotFound)
			return
		}
		b, err := os.ReadFile(filepath.Join(ReleasesDir(s.dir), "latest"))
		if err != nil {
			http.Error(w, "no release has been published yet", http.StatusServiceUnavailable)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte(strings.TrimSpace(string(b)) + "\n"))
		return
	}
	fsPath := filepath.Join(ReleasesDir(s.dir), ver, file)
	st, err := os.Stat(fsPath)
	if err != nil || st.IsDir() {
		http.Error(w, "release file not found", http.StatusNotFound)
		return
	}
	h := w.Header()
	if file == "SHA256SUMS" {
		h.Set("Content-Type", "text/plain; charset=utf-8")
	} else {
		h.Set("Content-Type", "application/octet-stream")
	}
	// Version dirs are immutable by publish contract (republish refused).
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, fsPath)
}

// requestHostRE admits exactly host[:port] shapes — DNS labels or an IPv4
// address, or a bracketed IPv6 literal, with an optional numeric port. The
// charset contains no shell metacharacters and no quotes, so a value that
// passes can never carry syntax into the single-quoted installer
// substitution.
var requestHostRE = regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?|\[[0-9A-Fa-f:.]+\])(:[0-9]{1,5})?$`)

// requestBase reconstructs the base URL the caller reached us on. The value
// is substituted into the executable install.sh, so it is strict by design:
// the scheme comes ONLY from the connection (plain http in tsnet mode — the
// tailnet encrypts transport; X-Forwarded-Proto is deliberately NOT honored,
// no trusted proxy exists in our topologies), and the Host header must
// validate against requestHostRE or the request is rejected.
func requestBase(r *http.Request) (string, error) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if !requestHostRE.MatchString(r.Host) {
		return "", fmt.Errorf("invalid Host header")
	}
	return scheme + "://" + r.Host, nil
}
