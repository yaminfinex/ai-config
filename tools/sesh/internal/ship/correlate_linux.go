//go:build linux

package ship

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sesh/internal/wire"
)

// PlatformCorrelator returns the /proc SESSION_OWNER correlator for the
// current uid (spec §4.2). Correlation is best-effort enrichment: it can
// only add an observation, and shipping never waits on or fails from it.
func PlatformCorrelator() func([]Discovered) map[string]string {
	c := &procCorrelator{Root: "/proc", UID: os.Getuid()}
	return c.CorrelateAll
}

const defaultCorrelationTTL = 10 * time.Second

// procCorrelator joins discovered session files to running processes through
// a short-lived observation cache. One shipper per OS user: any entry of
// another uid is rejected on its status line alone, before its environ or fd
// table is ever touched (S7/I9 — the cross-user wall is kernel-enforced at
// 0400, and never even attempting the read is what keeps a two-user node free
// of EACCES noise).
type procCorrelator struct {
	Root string // "/proc"; tests inject a fixture tree
	UID  int

	ttl            time.Duration
	now            func() time.Time
	cacheAt        time.Time
	cacheValid     bool
	cachedOwners   map[string]string
	lastIdentities map[string]struct{}
	scanCount      uint64
	readDir        func(string) ([]os.DirEntry, error)
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
	identities := make(map[string]struct{}, len(discovered))
	grew := false
	for _, d := range discovered {
		key := d.Identity.Key()
		identities[key] = struct{}{}
		if _, seen := c.lastIdentities[key]; !seen {
			grew = true
		}
	}
	if len(discovered) == 0 {
		c.lastIdentities = identities
		return nil
	}

	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	ttl := c.ttl
	if ttl <= 0 {
		ttl = defaultCorrelationTTL
	}
	if c.cacheValid && !grew && now.Before(c.cacheAt.Add(ttl)) {
		c.lastIdentities = identities
		return c.ownersFor(discovered)
	}

	entries := c.scan()
	owners := map[string]string{}
	if len(entries) > 0 {
		codexOwners := c.codexOwners(entries, discovered)
		for _, d := range discovered {
			var owner string
			var ok bool
			switch d.Identity.Tool {
			case wire.ToolCodex:
				owner, ok = codexOwners[d.Identity.Key()]
			case wire.ToolClaude:
				owner, ok = c.claudeOwner(entries, d.Path)
			case wire.ToolGrok:
				owner, ok = c.grokOwner(entries, d.Path)
			}
			if ok {
				owners[d.Identity.Key()] = owner
			}
		}
	}
	c.cacheAt = now
	c.cacheValid = true
	c.cachedOwners = owners
	c.lastIdentities = identities
	return owners
}

func (c *procCorrelator) ownersFor(discovered []Discovered) map[string]string {
	var owners map[string]string
	for _, d := range discovered {
		if owner := c.cachedOwners[d.Identity.Key()]; owner != "" {
			if owners == nil {
				owners = make(map[string]string)
			}
			owners[d.Identity.Key()] = owner
		}
	}
	return owners
}

// scan lists the same-uid process entries. The uid gate reads only the
// world-readable status file; entries of other uids contribute nothing and
// are never opened further.
func (c *procCorrelator) scan() []procEntry {
	c.scanCount++
	dirents, err := c.readDirectory(c.Root)
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

// codexOwners is the exact join (spec §4.2): pid → open fd → rollout file.
// When an inherited fd makes several processes hold the file, the leaf of
// the holder tree (the codex process itself, not its parent shell) names
// the owner; several distinct leaves must agree or nothing is stamped.
// Each process FD table is read once per sweep, independent of corpus size.
func (c *procCorrelator) codexOwners(entries []procEntry, discovered []Discovered) map[string]string {
	identitiesByPath := map[string][]string{}
	for _, d := range discovered {
		if d.Identity.Tool != wire.ToolCodex {
			continue
		}
		key := d.Identity.Key()
		identitiesByPath[d.Path] = append(identitiesByPath[d.Path], key)
		if resolved, err := filepath.EvalSymlinks(d.Path); err == nil && resolved != d.Path {
			identitiesByPath[resolved] = append(identitiesByPath[resolved], key)
		}
	}
	if len(identitiesByPath) == 0 {
		return nil
	}

	holdersByIdentity := map[string][]procEntry{}
	for _, e := range entries {
		fdDir := filepath.Join(c.Root, strconv.Itoa(e.pid), "fd")
		fds, err := c.readDirectory(fdDir)
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			for _, key := range identitiesByPath[target] {
				if !seen[key] {
					holdersByIdentity[key] = append(holdersByIdentity[key], e)
					seen[key] = true
				}
			}
		}
	}

	owners := map[string]string{}
	for key, holders := range holdersByIdentity {
		if owner, ok := c.codexOwner(holders); ok {
			owners[key] = owner
		}
	}
	return owners
}

func (c *procCorrelator) codexOwner(holders []procEntry) (string, bool) {
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

func (c *procCorrelator) readDirectory(path string) ([]os.DirEntry, error) {
	if c.readDir != nil {
		return c.readDir(path)
	}
	return os.ReadDir(path)
}

// claudeOwner is the cohort join (spec §4.2): candidate claude processes
// sharing (OS user, cwd) with the session's project dir must be unanimous
// on one SESSION_OWNER, else honest absence — same-cwd collisions are real
// and guessing is ruled worse than absence.
func (c *procCorrelator) claudeOwner(entries []procEntry, path string) (string, bool) {
	cohort := filepath.Base(filepath.Dir(path)) // the munged project cwd
	owner, cwd := "", ""
	for _, e := range entries {
		if e.comm != "claude" || e.cwd == "" || mungeCwd(e.cwd) != cohort {
			continue
		}
		if cwd != "" && e.cwd != cwd {
			// The slug is lossy: more than one DISTINCT actual cwd maps to
			// this session's project dir, and the file alone cannot say
			// which one it came from (the shipper never parses content).
			// Spec §4.2 cohorts by actual cwd; a wrong stamp is the worst
			// defect class here, so an unresolvable cohort is absence —
			// even when the colliding candidates agree on an owner.
			return "", false
		}
		cwd = e.cwd
		o := c.environOwner(e.pid)
		if o == "" || (owner != "" && o != owner) {
			return "", false // a candidate without the variable, or a collision
		}
		owner = o
	}
	return owner, owner != ""
}

// grokOwner is the cwd-cohort join for grok: the session path's cwd group
// (the transcript's grandparent directory name) is the percent-ENCODED
// working directory, so unlike the claude munge the decode is exact and the
// lossy-slug collision class does not exist. Candidate grok processes whose
// cwd equals the decoded group must be unanimous on one SESSION_OWNER, else
// honest absence — several grok runs share one cwd group by design (the
// group is not a seat-vs-manual discriminator).
func (c *procCorrelator) grokOwner(entries []procEntry, path string) (string, bool) {
	cwd, err := url.PathUnescape(filepath.Base(filepath.Dir(filepath.Dir(path))))
	if err != nil || cwd == "" {
		return "", false
	}
	owner := ""
	for _, e := range entries {
		if e.comm != "grok" || e.cwd != cwd {
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
