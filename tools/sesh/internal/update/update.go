// Package update implements `sesh update [--check]` (design §6): converge
// this node's installed binary AND its running service to the store's
// published latest. The base URL is the SESH_STORE_URL the node already
// couples on (no new config surface); the replacement target is the unit's
// pinned executable path, asserted equal to the running updater; the
// replacement ordering is crash-safe (the target path is never missing at
// any crash point); and the failure taxonomy distinguishes pre-restart
// failures (prior install untouched and running) from post-start
// verification failures (keep the forward binary, surface R23 verbatim).
package update

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sesh/internal/httpx"
	"sesh/internal/setup"
)

// ErrUpdateAvailable is returned by --check when latest differs from the
// running version. The CLI maps it to exit code 1 (0 = up to date,
// 2 = check failed), stable for scripting.
var ErrUpdateAvailable = errors.New("update available")

// versionRE pins the release version shape: git describe over sesh-v* tags
// (optionally suffixed -N-g<hash>), a generic vX.Y.Z, or a bare commit
// hash. Semver arity is exact — major.minor.patch, no more, no fewer.
// Anything else from the channel — including shape-adjacent corruption like
// the field bug's 'sesh-v0.1.0n' (a mangled publish; valid charset,
// nonexistent version) — is rejected loudly before it can become a fetch
// path or reach the filesystem. Kept in lockstep with scripts/release.sh
// and internal/store/assets/install.sh.
var versionRE = regexp.MustCompile(`^(sesh-)?v[0-9]+\.[0-9]+\.[0-9]+(-[0-9]+-g[0-9a-f]+)?$|^[0-9a-f]{7,40}$`)

// testHookAfterPrevLink runs between the prev-hardlink and the rename over
// the target — the crash window the injected-failure tests exercise.
var testHookAfterPrevLink func() error

// Options configures one update run. Zero values default to the real host
// environment; tests override the seams.
type Options struct {
	StoreURL string // explicit base URL (pre-setup use); default: installed config
	Check    bool

	OS      string // "linux" or "darwin"
	Arch    string // runtime.GOARCH
	Home    string
	Exe     string // resolved running binary; default: setup.ExecutablePath()
	Version string // running build version (buildinfo.Version)
	UID     int    // launchctl gui domain (darwin)

	Runner setup.Runner
	Client *http.Client
	Out    io.Writer
}

// Run performs one update pass and returns nil on success (including
// "already up to date").
func Run(opts Options) error {
	var err error
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Client == nil {
		// Bounded, never http.DefaultClient: a stalled release fetch would
		// otherwise hang the updater forever (the shipper-fallback wedge
		// class). Progress-sensitive, not wall-clock: the binary download is
		// the largest transfer in the codebase and must survive a slow
		// relayed link at any speed; only a zero-progress minute cuts it.
		opts.Client = httpx.NewBulkClient(time.Minute, 2)
	}
	if opts.Runner == nil {
		opts.Runner = setup.NewExecRunner()
	}
	if opts.Home == "" {
		if opts.Home, err = os.UserHomeDir(); err != nil {
			return err
		}
	}
	if opts.Exe == "" {
		if opts.Exe, err = setup.ExecutablePath(); err != nil {
			return fmt.Errorf("cannot resolve the running binary: %w", err)
		}
	}

	base, err := resolveBaseURL(opts)
	if err != nil {
		return err
	}
	target, hasService, err := resolveTarget(opts)
	if err != nil {
		return err
	}

	// Stray temp files from an interrupted earlier run have defined
	// recovery: delete them on the next run.
	if stale, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".sesh-update-*.tmp")); len(stale) > 0 {
		for _, f := range stale {
			_ = os.Remove(f)
		}
	}

	// latest is read exactly ONCE; all further fetches use immutable
	// /releases/<ver>/ paths. Version semantics are equality-only:
	// git-describe strings have no defined ordering and none is invented,
	// so an operator's deliberate latest rollback propagates as a visible
	// fleet downgrade.
	latest, err := fetchLatestVersion(opts.Client, base)
	if err != nil {
		return err
	}
	if latest == opts.Version {
		fmt.Fprintf(opts.Out, "already up to date: %s\n", opts.Version)
		return nil
	}
	if opts.Check {
		fmt.Fprintf(opts.Out, "update available: %s -> %s (run `sesh update`)\n", opts.Version, latest)
		return ErrUpdateAvailable
	}
	// from -> to is printed unconditionally: a downgrade is a feature
	// (deliberate latest rollback) but never a silent one.
	fmt.Fprintf(opts.Out, "updating sesh: %s -> %s (target %s)\n", opts.Version, latest, target)

	// --- everything below here up to the restart leaves the prior install
	// untouched and running on failure (full rollback guaranteed) ---

	asset := fmt.Sprintf("sesh-%s-%s", opts.OS, opts.Arch)
	sum, err := fetchChecksum(opts.Client, base, latest, asset)
	if err != nil {
		return err
	}
	tmp, err := downloadVerified(opts.Client, base, latest, asset, sum, filepath.Dir(target))
	if err != nil {
		return err
	}
	defer os.Remove(tmp) // harmless no-op once the rename succeeds

	if err := replaceBinary(tmp, target); err != nil {
		return err
	}

	if !hasService {
		fmt.Fprintf(opts.Out, "updated: %s -> %s (no installed service to restart; run `sesh setup` to install one)\n", opts.Version, latest)
		return nil
	}
	if err := restartService(opts); err != nil {
		// The binary on disk is the new one; never claim a rollback that
		// did not happen.
		return fmt.Errorf("binary updated to %s but the service restart failed: %w", latest, err)
	}
	if err := verifyRunning(opts, target, latest); err != nil {
		return err
	}
	fmt.Fprintf(opts.Out, "updated: %s -> %s; service restarted and verified\n", opts.Version, latest)
	return nil
}

// resolveBaseURL returns the store URL: the explicit flag, else the one the
// node already couples on from the installed per-OS config.
func resolveBaseURL(opts Options) (string, error) {
	if opts.StoreURL != "" {
		return strings.TrimRight(opts.StoreURL, "/"), nil
	}
	url, path, ok := setup.InstalledStoreURL(opts.OS, opts.Home)
	if !ok {
		return "", fmt.Errorf("no store URL: %s has none and --store-url was not passed (run `sesh setup` first)", path)
	}
	return strings.TrimRight(url, "/"), nil
}

// resolveTarget returns the file `sesh update` may replace: the service's
// pinned executable path, which must be this very binary. Without an
// installed service (pre-setup use) the updater replaces itself and reports
// hasService=false so the restart+verify half is skipped.
func resolveTarget(opts Options) (target string, hasService bool, err error) {
	var pinned string
	var ok bool
	switch opts.OS {
	case "darwin":
		content, readErr := os.ReadFile(setup.PlistPath(opts.Home))
		if readErr != nil {
			return opts.Exe, false, nil
		}
		pinned, ok = setup.PlistExecPath(content)
	default:
		content, readErr := os.ReadFile(setup.UnitPath(opts.Home))
		if readErr != nil {
			return opts.Exe, false, nil
		}
		pinned, ok = setup.UnitExecPath(content)
	}
	if !ok {
		return "", false, fmt.Errorf("installed service config carries no executable path; re-run `sesh setup`")
	}
	resolvedPinned, err := filepath.EvalSymlinks(pinned)
	if err != nil {
		return "", false, fmt.Errorf("refusing to update: cannot prove this binary (%s) matches the service's pinned executable (%s): cannot resolve the pinned path: %w — re-run `sesh setup` to repin", opts.Exe, pinned, err)
	}
	resolvedRunning, err := filepath.EvalSymlinks(opts.Exe)
	if err != nil {
		return "", false, fmt.Errorf("refusing to update: cannot prove this binary (%s) matches the service's pinned executable (%s): cannot resolve the running path: %w — re-run `sesh setup` to repin", opts.Exe, pinned, err)
	}
	// Executable identity is compared after resolving both path spellings.
	// Setup normally pins an already-resolved path, but older pins and paths
	// beneath symlinked temporary roots must remain valid without rewriting the
	// installed service config. Resolution failure is a closed guard: an
	// unprovable identity must never authorize replacement of the pinned file.
	if resolvedPinned != resolvedRunning {
		return "", false, fmt.Errorf("refusing to update: this binary (%s) is not the service's pinned executable (%s) — run that binary's `sesh update`, or re-run `sesh setup` to repin", resolvedRunning, resolvedPinned)
	}
	return resolvedPinned, true, nil
}

func fetchLatestVersion(client *http.Client, base string) (string, error) {
	resp, err := client.Get(base + "/releases/latest/VERSION")
	if err != nil {
		return "", fmt.Errorf("could not check for updates: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return "", fmt.Errorf("the store at %s has not published a release yet", base)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s/releases/latest/VERSION: %s", base, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	ver := strings.TrimSpace(string(b))
	if !versionRE.MatchString(ver) {
		return "", fmt.Errorf("invalid version string from %s/releases/latest/VERSION: %q — refusing to build a download URL from it", base, ver)
	}
	return ver, nil
}

func fetchChecksum(client *http.Client, base, ver, asset string) (string, error) {
	resp, err := client.Get(base + "/releases/" + ver + "/SHA256SUMS")
	if err != nil {
		return "", fmt.Errorf("could not fetch SHA256SUMS: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET /releases/%s/SHA256SUMS: %s", ver, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && (fields[1] == asset || fields[1] == "*"+asset) {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS for %s has no entry for %s", ver, asset)
}
