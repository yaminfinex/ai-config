package sidecarcmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Sidecars are content-hash-keyed cache binaries that otherwise outlive every
// herder update (the pane holds them for days). maybeReexec follows the same
// last-good pointer bin/herder maintains and syscall.Execs into the newer
// build: the PID (and therefore the holder's PPID watch chain), open log fds,
// env, and args all survive, so death recording is never interrupted.
func (s *sidecar) maybeReexec() {
	now := time.Now()
	if now.Before(s.reexecCheckAt) {
		return
	}
	s.reexecCheckAt = now.Add(time.Minute)
	self, target := reexecTarget()
	if target == "" {
		return
	}
	s.diagTransition("reexec", fmt.Sprintf("re-exec into updated build %s", filepath.Base(target)))
	argv := append([]string{target}, os.Args[1:]...)
	if err := syscall.Exec(target, argv, os.Environ()); err != nil {
		s.diagTransition("reexec", fmt.Sprintf("re-exec into %s failed: %v; staying on %s", target, err, self))
	}
}

// reexecTarget resolves the checkout's current last-good binary. It returns
// empty when this process is not itself a cache binary — a test binary or a
// hand-built herder must never exec away — or when the pointer already names
// the running build.
func reexecTarget() (self, target string) {
	root := os.Getenv("AI_CONFIG_ROOT")
	if root == "" {
		return "", ""
	}
	self, err := os.Executable()
	if err != nil {
		return "", ""
	}
	candidates := cacheCandidates()
	if !dirIsAmong(filepath.Dir(self), candidates) {
		return self, ""
	}
	srcDir := filepath.Join(root, "tools", "herder")
	sum := sha256.Sum256([]byte(srcDir))
	pointerName := ".herder-lastgood-" + hex.EncodeToString(sum[:])[:16]
	for _, dir := range candidates {
		b, err := os.ReadFile(filepath.Join(dir, pointerName))
		if err != nil {
			continue
		}
		candidate := strings.TrimSpace(string(b))
		if candidate == "" || !ownedExecutable(candidate) {
			continue
		}
		if sameFile(candidate, self) {
			return self, ""
		}
		return self, candidate
	}
	return self, ""
}

// cacheCandidates mirrors bin/herder's cache walk order exactly.
func cacheCandidates() []string {
	var out []string
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		out = append(out, filepath.Join(xdg, "herder"))
	}
	if home := os.Getenv("HOME"); home != "" {
		out = append(out, filepath.Join(home, ".cache", "herder"))
	}
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	out = append(out, filepath.Join(tmp, "herder-cache-"+strconv.Itoa(os.Getuid()), "herder"))
	return out
}

func dirIsAmong(dir string, candidates []string) bool {
	for _, candidate := range candidates {
		if sameFile(dir, candidate) {
			return true
		}
	}
	return false
}

func ownedExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}
