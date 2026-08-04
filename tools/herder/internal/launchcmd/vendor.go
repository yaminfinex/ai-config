package launchcmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Vendor resolution for claude/codex: the launch path must never depend on
// machine-wide PATH *ordering*. hcom resolves the tool by bare name from the
// PATH it inherits, and that order is rewritten concurrently by mise
// hook-env (on every config-boundary cd), rc files, and installer prepends —
// the race that produced off-bus agents without their autonomous flags.
// resolveVendorTool walks PATH once with a skip-list, and vendorPinDir
// materializes a single-entry bin dir that launch prepends to the CHILD env
// only, so hcom's own lookup has exactly one deterministic answer. Grok has
// its own equivalent (resolveGrokBinary); pi keeps plain LookPath.

const herderShimMarker = "herder-path-shim"

// resolveVendorTool returns the first PATH candidate that is a real vendor
// binary: not a herder path shim (any checkout — detected by the marker line
// in its head), and not anything mise-owned. The mise skip covers both the
// auto-regenerated dispatcher shims in mise's shims dir and accidental npm
// copies inside mise-managed installs (node/*/bin/claude): the vendor
// installer never targets a mise-owned path, so a mise-owned candidate is
// always the wrong one.
func resolveVendorTool(tool string) (string, error) {
	if tool == "" {
		return "", errors.New("vendor resolve: empty tool name")
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, tool)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if isHerderShimFile(abs) || isMiseOwned(abs) {
			continue
		}
		return abs, nil
	}
	return "", fmt.Errorf("no vendor '%s' on PATH after skipping herder shims and mise-owned copies; install the vendor CLI (e.g. into ~/.local/bin) and retry", tool)
}

// isHerderShimFile reports whether the file's head carries the herder shim
// marker. Content beats path comparison: a sibling checkout's shim dir must
// be skipped just like this checkout's.
func isHerderShimFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := f.Read(head)
	return strings.Contains(string(head[:n]), herderShimMarker)
}

// isMiseOwned reports whether the candidate lives under a mise tree (shims or
// installs), either directly or through its symlink chain, or dispatches to
// the mise binary itself.
func isMiseOwned(path string) bool {
	if pathHasMiseSegment(path) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		if pathHasMiseSegment(resolved) {
			return true
		}
		base := filepath.Base(resolved)
		if base == "mise" || strings.HasPrefix(base, "mise-") {
			return true
		}
	}
	return false
}

func pathHasMiseSegment(path string) bool {
	norm := filepath.ToSlash(path)
	return strings.Contains(norm, "/mise/shims/") || strings.Contains(norm, "/mise/installs/")
}

// vendorPinDir ensures <cache>/herder/vendorbin/<tool>/<tool> is a symlink to
// vendorPath and returns the directory, for prepending to the launch child's
// PATH. The symlink targets the stable vendor entry point (e.g.
// ~/.local/bin/claude, itself a symlink the vendor updater retargets), so a
// vendor self-update never invalidates the pin.
func vendorPinDir(tool, vendorPath string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "herder", "vendorbin", tool)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	link := filepath.Join(dir, tool)
	if current, err := os.Readlink(link); err == nil && current == vendorPath {
		return dir, nil
	}
	_ = os.Remove(link)
	if err := os.Symlink(vendorPath, link); err != nil {
		return "", err
	}
	return dir, nil
}

// prependPathEnv returns env with dir prepended to its PATH entry (or a new
// PATH entry if none exists). The input slice is not modified.
func prependPathEnv(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	seen := false
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok && key == "PATH" && !seen {
			seen = true
			out = append(out, "PATH="+dir+string(os.PathListSeparator)+value)
			continue
		}
		out = append(out, item)
	}
	if !seen {
		out = append(out, "PATH="+dir)
	}
	return out
}

// pinVendorForLaunch resolves the vendor binary for tools whose launch goes
// through hcom's bare-name lookup and returns launchEnv with the pin dir
// fronting PATH. Resolution failure is a WARNING, not fatal: a machine still
// on the previous generation (shims dir on global PATH) launches fine through
// the shim's own resolution, and hcom fails loudly itself when nothing
// resolves. The doctor owns turning this into a finding.
func pinVendorForLaunch(tool string, launchEnv []string, stderr interface{ Write([]byte) (int, error) }) []string {
	if tool != "claude" && tool != "codex" {
		return launchEnv
	}
	vendor, err := resolveVendorTool(tool)
	if err != nil {
		fmt.Fprintln(stderr, "herder launch: "+err.Error()+" (continuing with unpinned PATH resolution)")
		return launchEnv
	}
	dir, err := vendorPinDir(tool, vendor)
	if err != nil {
		fmt.Fprintln(stderr, "herder launch: could not pin vendor "+tool+" ("+err.Error()+"); continuing with unpinned PATH resolution")
		return launchEnv
	}
	return prependPathEnv(launchEnv, dir)
}
