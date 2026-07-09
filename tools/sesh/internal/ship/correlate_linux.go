//go:build linux

package ship

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"sesh/internal/wire"
)

// PlatformCorrelator returns the /proc SESSION_OWNER correlator for the
// current uid (spec §4.2). Correlation is best-effort enrichment: it can
// only add an observation, and shipping never waits on or fails from it.
func PlatformCorrelator() func([]Discovered) map[string]string {
	c := &procCorrelator{Root: "/proc", UID: os.Getuid()}
	return c.CorrelateAll
}

// procCorrelator joins discovered session files to running processes through
// one /proc scan per pass. One shipper per OS user: any entry of another uid
// is rejected on its status line alone, before its environ or fd table is
// ever touched (S7/I9 — the cross-user wall is kernel-enforced at 0400, and
// never even attempting the read is what keeps a two-user node free of
// EACCES noise).
type procCorrelator struct {
	Root string // "/proc"; tests inject a fixture tree
	UID  int
}

type procEntry struct {
	pid  int
	ppid int
	comm string
	cwd  string // resolved target of the cwd link; "" when unreadable
}

// CorrelateAll answers identity key → observed SESSION_OWNER for one
// authoritative pass. Identities it cannot stamp are simply absent: honest
// absence is a result, never an error, and nothing here logs — a dying
// process or a procfs race reads exactly like absence (I9).
func (c *procCorrelator) CorrelateAll(discovered []Discovered) map[string]string {
	entries := c.scan()
	if len(entries) == 0 {
		return nil
	}
	owners := map[string]string{}
	for _, d := range discovered {
		var owner string
		var ok bool
		switch d.Identity.Tool {
		case wire.ToolCodex:
			owner, ok = c.codexOwner(entries, d.Path)
		case wire.ToolClaude:
			owner, ok = c.claudeOwner(entries, d.Path)
		}
		if ok {
			owners[d.Identity.Key()] = owner
		}
	}
	return owners
}

// scan lists the same-uid process entries. The uid gate reads only the
// world-readable status file; entries of other uids contribute nothing and
// are never opened further.
func (c *procCorrelator) scan() []procEntry {
	dirents, err := os.ReadDir(c.Root)
	if err != nil {
		return nil
	}
	var entries []procEntry
	for _, de := range dirents {
		pid, err := strconv.Atoi(de.Name())
		if err != nil {
			continue // self, sys, ... — not a process entry
		}
		uid, ppid, ok := c.statusIdentity(pid)
		if !ok || uid != c.UID {
			continue
		}
		dir := filepath.Join(c.Root, de.Name())
		comm, _ := os.ReadFile(filepath.Join(dir, "comm"))
		cwd, _ := os.Readlink(filepath.Join(dir, "cwd"))
		entries = append(entries, procEntry{pid: pid, ppid: ppid, comm: strings.TrimSpace(string(comm)), cwd: cwd})
	}
	return entries
}

// statusIdentity reads the real uid and parent pid from /proc/<pid>/status.
func (c *procCorrelator) statusIdentity(pid int) (uid, ppid int, ok bool) {
	raw, err := os.ReadFile(filepath.Join(c.Root, strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, 0, false
	}
	uid, ppid = -1, -1
	for line := range strings.Lines(string(raw)) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "Uid:":
			uid, _ = strconv.Atoi(f[1]) // first field: real uid
		case "PPid:":
			ppid, _ = strconv.Atoi(f[1])
		}
	}
	return uid, ppid, uid >= 0
}

// codexOwner is the exact join (spec §4.2): pid → open fd → rollout file.
// When an inherited fd makes several processes hold the file, the leaf of
// the holder tree (the codex process itself, not its parent shell) names
// the owner; several distinct leaves must agree or nothing is stamped.
func (c *procCorrelator) codexOwner(entries []procEntry, path string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	var holders []procEntry
	for _, e := range entries {
		fdDir := filepath.Join(c.Root, strconv.Itoa(e.pid), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if target == resolved || target == path {
				holders = append(holders, e)
				break
			}
		}
	}
	if len(holders) == 0 {
		return "", false
	}
	// Drop any holder that is an ancestor of another holder; what remains
	// are the leaves.
	holderPids := map[int]bool{}
	for _, h := range holders {
		holderPids[h.pid] = true
	}
	ancestors := map[int]bool{}
	for _, h := range holders {
		for p, hops := h.ppid, 0; p > 1 && hops < 64; hops++ {
			if holderPids[p] {
				ancestors[p] = true
			}
			_, pp, ok := c.statusIdentity(p)
			if !ok {
				break
			}
			p = pp
		}
	}
	owner := ""
	for _, h := range holders {
		if ancestors[h.pid] {
			continue
		}
		o := c.environOwner(h.pid)
		if o == "" || (owner != "" && o != owner) {
			return "", false // leaf without an owner, or disagreeing leaves
		}
		owner = o
	}
	return owner, owner != ""
}

// claudeOwner is the cohort join (spec §4.2): candidate claude processes
// sharing (OS user, cwd) with the session's project dir must be unanimous
// on one SESSION_OWNER, else honest absence — same-cwd collisions are real
// and guessing is ruled worse than absence.
func (c *procCorrelator) claudeOwner(entries []procEntry, path string) (string, bool) {
	cohort := filepath.Base(filepath.Dir(path)) // the munged project cwd
	owner := ""
	for _, e := range entries {
		if e.comm != "claude" || e.cwd == "" || mungeCwd(e.cwd) != cohort {
			continue
		}
		o := c.environOwner(e.pid)
		if o == "" || (owner != "" && o != owner) {
			return "", false // a candidate without the variable, or a collision
		}
		owner = o
	}
	return owner, owner != ""
}

// environOwner extracts SESSION_OWNER from a same-uid process's environ.
// A read failure here is a death race (or, defensively, a permission
// surprise) and yields silent absence.
func (c *procCorrelator) environOwner(pid int) string {
	raw, err := os.ReadFile(filepath.Join(c.Root, strconv.Itoa(pid), "environ"))
	if err != nil {
		return ""
	}
	for _, kv := range strings.Split(string(raw), "\x00") {
		if v, ok := strings.CutPrefix(kv, "SESSION_OWNER="); ok {
			return v
		}
	}
	return ""
}

// mungeCwd reproduces the claude project-dir naming: every byte outside
// [A-Za-z0-9] becomes '-'. The munge is lossy by design; it only needs to
// agree with what claude itself wrote as the directory name.
var mungeRe = regexp.MustCompile(`[^A-Za-z0-9]`)

func mungeCwd(cwd string) string { return mungeRe.ReplaceAllString(cwd, "-") }
