package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sesh/internal/setup"
)

// downloadVerified streams the immutable release asset into a temp file in
// dir (the target's directory, so the later rename stays on one filesystem),
// verifies its SHA256SUMS entry, syncs, and returns the temp path.
func downloadVerified(client *http.Client, base, ver, asset, wantSum, dir string) (string, error) {
	resp, err := client.Get(base + "/releases/" + ver + "/" + asset)
	if err != nil {
		return "", fmt.Errorf("could not download %s: %v", asset, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET /releases/%s/%s: %s", ver, asset, resp.Status)
	}

	tmp, err := os.CreateTemp(dir, ".sesh-update-*.tmp")
	if err != nil {
		return "", fmt.Errorf("cannot write to %s: %v (is the install directory writable?)", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = tmp.Close(); _ = os.Remove(tmpName) }

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), resp.Body); err != nil {
		cleanup()
		return "", fmt.Errorf("downloading %s: %v", asset, err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != wantSum {
		cleanup()
		return "", fmt.Errorf("checksum mismatch for %s (got %s, want %s) — aborting, nothing was replaced", asset, got, wantSum)
	}
	// The executable mode is set BEFORE the final fsync so it sits inside
	// the durability chain: after a crash the renamed binary can never be
	// durable-in-content but non-executable-in-mode.
	if err := tmp.Chmod(0o755); err != nil {
		cleanup()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", err
	}
	return tmpName, nil
}

// replaceBinary installs tmp over target with the crash-safe ordering
// (design §6.4): first retain the current binary as <target>.prev via
// hardlink (copy fallback) — the target path itself is NEVER unlinked — then
// atomically rename tmp over target and fsync the directory. Every partial
// state has defined recovery: stray temp files are deleted on the next run,
// a .prev with an intact target is inert, and the target exists at every
// crash point.
func replaceBinary(tmp, target string) error {
	prev := target + ".prev"
	if err := os.Remove(prev); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot clear %s: %v", prev, err)
	}
	if err := os.Link(target, prev); err != nil {
		// Filesystems without hardlinks: retain a copy instead.
		if copyErr := copyFile(target, prev); copyErr != nil {
			return fmt.Errorf("cannot retain the previous binary as %s: %v", prev, copyErr)
		}
	}
	if err := syncDir(filepath.Dir(target)); err != nil {
		return err
	}
	if testHookAfterPrevLink != nil {
		if err := testHookAfterPrevLink(); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("installing update to %s: %v", target, err)
	}
	return syncDir(filepath.Dir(target))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// restartService restarts the shipper unit so the running image converges
// with the on-disk binary. Updated-but-not-restarted is failure, not success.
func restartService(opts Options) error {
	if opts.OS == "darwin" {
		return opts.Runner.Run("launchctl", "kickstart", "-k", setup.LaunchdServiceTarget(opts.UID))
	}
	return opts.Runner.Run("systemctl", "--user", "restart", setup.ServiceName)
}

// r23Signature is the stable head of the R23 stale-binary refusal; when the
// restarted service dies with it, the update reports failed-but-forward and
// surfaces the message verbatim (design §6.6): the new binary may already
// have migrated the cursor registry, so the old binary would now correctly
// refuse — rolling back is a promise the updater cannot keep, and the
// refused old binary must never be left as the service target.
const r23Signature = "cursor registry"

// verifyPollInterval × verifyPollAttempts bounds how long verifyRunning
// waits for the restarted unit to expose a stable running image.
var (
	verifyPollInterval = 300 * time.Millisecond
	verifyPollAttempts = 10
)

// verifyRunning confirms the RUNNING image reports the new version. Linux
// reads /proc/<MainPID>/exe (the same mechanism as `just versions`); macOS
// can only check the on-disk target until the service restarts — the
// README's stated caveat — so it executes the target path directly.
func verifyRunning(opts Options, target, want string) error {
	if opts.OS == "darwin" {
		got, err := opts.Runner.Output(target, "version")
		if err != nil {
			return forwardFailure(opts, want, fmt.Sprintf("on-disk binary did not report a version: %v", err))
		}
		if strings.TrimSpace(got) != want {
			return forwardFailure(opts, want, fmt.Sprintf("on-disk binary reports %q, want %q", strings.TrimSpace(got), want))
		}
		fmt.Fprintln(opts.Out, "note (macOS): verified on disk; the running image is confirmed by launchctl once the kickstart settles")
		return nil
	}
	var lastReason string
	for attempt := 0; attempt < verifyPollAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(verifyPollInterval)
		}
		pid, err := opts.Runner.Output("systemctl", "--user", "show", setup.ServiceName, "--property=MainPID", "--value")
		if err != nil {
			lastReason = fmt.Sprintf("cannot read the unit's MainPID: %v", err)
			continue
		}
		pid = strings.TrimSpace(pid)
		if pid == "" || pid == "0" {
			lastReason = "the unit has no running main process"
			continue
		}
		got, err := opts.Runner.Output("/proc/"+pid+"/exe", "version")
		if err != nil {
			lastReason = fmt.Sprintf("running image did not report a version: %v", err)
			continue
		}
		if strings.TrimSpace(got) == want {
			return nil
		}
		lastReason = fmt.Sprintf("running image reports %q, want %q", strings.TrimSpace(got), want)
	}
	return forwardFailure(opts, want, lastReason)
}

// forwardFailure reports a post-start verification failure per the §6
// taxonomy: the (compatible) new binary stays in place, any R23 refusal from
// the service log is surfaced verbatim, and the update is reported as
// failed-but-forward — never as a rollback it cannot deliver.
func forwardFailure(opts Options, want, reason string) error {
	msg := fmt.Sprintf("update failed-but-forward: the binary at the service target is now %s but the running service could not be verified (%s). The new binary stays in place (it may already have migrated node state; the previous binary would be refused). Inspect the service log and restart manually.", want, reason)
	if opts.OS != "darwin" {
		if log, err := opts.Runner.Output("journalctl", "--user", "-u", setup.ServiceName, "-n", "50", "--no-pager"); err == nil {
			if r23 := extractR23(log); r23 != "" {
				msg += "\nservice log (verbatim):\n" + r23
			}
		}
	}
	return fmt.Errorf("%s", msg)
}

// extractR23 pulls the R23 refusal lines out of recent journal output.
func extractR23(log string) string {
	var lines []string
	capture := false
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, r23Signature) {
			capture = true
		}
		if capture {
			lines = append(lines, line)
			if strings.Contains(line, "left untouched") {
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}
