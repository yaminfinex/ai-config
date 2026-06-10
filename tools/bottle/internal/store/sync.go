package store

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"ai-config/tools/bottle/internal/refs"
)

// This file is the registry-projection half of `bottle sync`: after a git
// merge unions two stores' bottle dirs, the names map is rebuilt from the
// metas alone, so both machines converge on byte-identical registries
// regardless of sync order. projectNames is pure (no I/O); scanMetas and
// applyProjection are its store-side bookends.

// SyncRename is one collision-policy decision: the bottle that lost a
// name@version collision and the suffixed name its name-group moved to.
type SyncRename struct {
	BottleID string
	OldName  string
	NewName  string
}

// projectNames deterministically rebuilds the registry names map from the
// union of bottle metas. A collision is two ids claiming the same
// name@version; the older Created keeps the name (tie → smaller id), and the
// loser moves — with every same-named descendant stacked on it — to the
// first free suffixed name, keeping version numbers and parent pointers.
// Pure: no I/O, deterministic for any input order.
func projectNames(bottles []Bottle) (map[string][]versionEntry, []SyncRename, error) {
	byID := make(map[string]Bottle, len(bottles))
	curName := make(map[string]string, len(bottles)) // id → projected name
	taken := make(map[string]bool)                   // every name post-union, plus rename targets
	children := make(map[string][]string)            // parent id → child ids
	ids := make([]string, 0, len(bottles))
	for _, b := range bottles {
		if _, dup := byID[b.ID]; dup {
			continue // ids are unique on disk; tolerate a duplicated input pair
		}
		byID[b.ID] = b
		curName[b.ID] = b.Meta.Name
		taken[b.Meta.Name] = true
		if p := b.Meta.Parent; p != nil {
			children[p.BottleID] = append(children[p.BottleID], b.ID)
		}
		ids = append(ids, b.ID)
	}
	sort.Strings(ids)

	var renames []SyncRename
	for {
		name, losers := firstCollision(ids, byID, curName)
		if name == "" {
			break
		}
		for _, loser := range losers {
			if curName[loser] != name {
				continue // already moved as part of an earlier loser's group
			}
			newName, err := firstFreeSuffix(name, taken)
			if err != nil {
				return nil, nil, err
			}
			taken[newName] = true
			for _, id := range nameGroup(loser, name, byID, curName, children) {
				curName[id] = newName
				renames = append(renames, SyncRename{BottleID: id, OldName: name, NewName: newName})
			}
		}
	}

	names := make(map[string][]versionEntry, len(taken))
	for _, id := range ids {
		n := curName[id]
		names[n] = append(names[n], versionEntry{Version: byID[id].Meta.Version, BottleID: id})
	}
	for _, entries := range names {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Version != entries[j].Version {
				return entries[i].Version < entries[j].Version
			}
			return entries[i].BottleID < entries[j].BottleID
		})
	}
	return names, renames, nil
}

// firstCollision finds the lowest (name, version) claimed by two or more ids
// under the current projection and returns the name plus the losing ids,
// worst-ranked last. An empty name means no collisions remain. Iterating
// collisions in this fixed order is what keeps resolution order-independent.
func firstCollision(ids []string, byID map[string]Bottle, curName map[string]string) (string, []string) {
	type key struct {
		name    string
		version int
	}
	groups := make(map[key][]string)
	for _, id := range ids {
		k := key{curName[id], byID[id].Meta.Version}
		groups[k] = append(groups[k], id)
	}
	var first key
	for k, members := range groups {
		if len(members) < 2 {
			continue
		}
		if first.name == "" || k.name < first.name || (k.name == first.name && k.version < first.version) {
			first = k
		}
	}
	if first.name == "" {
		return "", nil
	}
	contenders := groups[first]
	sort.Slice(contenders, func(i, j int) bool {
		ci, cj := byID[contenders[i]].Meta.Created, byID[contenders[j]].Meta.Created
		if !ci.Equal(cj) {
			return ci.Before(cj) // older Created keeps the name
		}
		return contenders[i] < contenders[j] // tie → smaller id
	})
	return first.name, contenders[1:]
}

// nameGroup collects the loser and every transitive descendant (by
// Parent.BottleID) still carrying the collided name — the whole branch moves
// together so stacked rebottles keep their version numbers and lineage.
func nameGroup(loserID, name string, byID map[string]Bottle, curName map[string]string, children map[string][]string) []string {
	group := []string{loserID}
	seen := map[string]bool{loserID: true}
	for i := 0; i < len(group); i++ {
		for _, kid := range children[group[i]] {
			if !seen[kid] && curName[kid] == name {
				seen[kid] = true
				group = append(group, kid)
			}
		}
	}
	sort.Slice(group, func(i, j int) bool {
		vi, vj := byID[group[i]].Meta.Version, byID[group[j]].Meta.Version
		if vi != vj {
			return vi < vj
		}
		return group[i] < group[j]
	})
	return group
}

// firstFreeSuffix returns the first base-2, base-3, … name not yet taken.
// Suffixed names already satisfy the name grammar; validation is defensive.
func firstFreeSuffix(base string, taken map[string]bool) (string, error) {
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if taken[candidate] {
			continue
		}
		if err := refs.ValidateName(candidate); err != nil {
			return "", fmt.Errorf("cannot allocate a collision rename for %q: %w", base, err)
		}
		return candidate, nil
	}
}

// scanMetas loads every bottle meta under store/ — the projection's input
// after a sync merge unions both machines' bottle dirs.
func (s *Store) scanMetas() ([]Bottle, error) {
	paths, err := s.backend.List("store")
	if err != nil {
		return nil, err
	}
	var bottles []Bottle
	for _, p := range paths {
		dir, file, ok := strings.Cut(p, "/")
		if !ok || file != "meta.json" {
			continue
		}
		b, err := s.Get(dir)
		if err != nil {
			return nil, err
		}
		bottles = append(bottles, *b)
	}
	return bottles, nil
}

// applyProjection makes a projection durable: each rename rewrites the loser
// meta via the rename idiom (old name appended to PreviousNames, Name set),
// then the projected names map replaces the registry's and is written through
// AtomicSwap. The caller holds the exclusive lock and owns the registry's
// decants (sync unions them before calling).
func (s *Store) applyProjection(reg *registry, names map[string][]versionEntry, renames []SyncRename) error {
	for _, rn := range renames {
		b, err := s.Get(rn.BottleID)
		if err != nil {
			return err
		}
		b.Meta.PreviousNames = append(b.Meta.PreviousNames, rn.OldName)
		b.Meta.Name = rn.NewName
		if err := s.writeMeta(rn.BottleID, b.Meta); err != nil {
			return err
		}
	}
	reg.Names = names
	return s.writeRegistry(reg)
}

// SyncReport is what one Sync run did, shaped for the CLI to render: bottle
// counts each way, the collision renames applied locally (deduped to one
// entry per old→new name move), and the remote in effect. The report covers
// this run only — renames committed by a previously-interrupted sync are not
// re-reported by the run that heals it.
type SyncReport struct {
	Received         int          // bottles that arrived from the remote
	Sent             int          // bottles the remote lacked before this sync
	Renames          []SyncRename // one entry per (OldName, NewName) pair
	RemoteURL        string       // origin's URL after this run
	RemoteConfigured bool         // this call set or replaced origin (--remote)
}

// Sync converges this store with its git remote: sweep dirty state → fetch →
// merge → re-project the registry from the unioned metas → union decants
// (ours wins) → commit → push. A non-empty remoteURL configures (or
// replaces) origin first. Sync is the only store operation that touches the
// network; any failure aborts the in-progress merge and leaves the store as
// it was. A sync interrupted between merge-commit and push self-heals on the
// next run (origin is already an ancestor, so it just pushes).
func (s *Store) Sync(remoteURL string) (SyncReport, error) {
	var rep SyncReport
	if !s.config.gitEnabled() {
		return rep, fmt.Errorf("sync needs the git substrate, but this store disables it (git_auto_commit: false in config.json)")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return rep, fmt.Errorf("sync needs git, which is not on PATH")
	}
	unlock, err := s.lockExclusive()
	if err != nil {
		return rep, err
	}
	defer unlock()

	if err := s.initRepoIfNeeded(gitPath); err != nil {
		return rep, err
	}
	if remoteURL != "" {
		if err := s.setOrigin(gitPath, remoteURL); err != nil {
			return rep, err
		}
		rep.RemoteConfigured = true
	}
	url, ok := s.originURL(gitPath)
	if !ok {
		return rep, fmt.Errorf("no remote configured — run: bottle sync --remote <url>")
	}
	rep.RemoteURL = url

	// A prior process may have died between merge --no-commit and its commit
	// or abort, leaving MERGE_HEAD behind; abort it (best-effort, a no-op
	// when no merge is in progress) so the sweep below never concludes a
	// stale merge with conflict markers still in the worktree.
	s.git(gitPath, "merge", "--abort")
	// Sweep anything a crashed or git-less mutation left uncommitted, so the
	// merge starts from a clean worktree.
	if err := s.commitAll(gitPath, "sync: sweep local state"); err != nil {
		return rep, err
	}
	branch, err := s.currentBranch(gitPath)
	if err != nil {
		return rep, err
	}
	// --prune drops remote-tracking refs the new origin no longer has, so a
	// --remote re-pointing never fast-paths or merges against the old
	// remote's stale refs.
	if out, err := s.git(gitPath, "fetch", "--prune", "origin"); err != nil {
		return rep, fmt.Errorf("fetch from %s failed: %s", url, firstLine(out))
	}
	remoteRef := "origin/" + branch

	preIDs, err := s.localBottleIDs()
	if err != nil {
		return rep, err
	}

	// Empty remote: nothing to merge, push everything.
	if _, err := s.git(gitPath, "rev-parse", "--verify", "--quiet", "refs/remotes/"+remoteRef); err != nil {
		if err := s.push(gitPath, branch, url); err != nil {
			return rep, err
		}
		rep.Sent = len(preIDs)
		return rep, nil
	}

	theirIDs, err := s.remoteBottleIDs(gitPath, remoteRef)
	if err != nil {
		return rep, err
	}

	// origin/<branch> already merged in (or never diverged): just push — this
	// is also how an interrupted sync self-heals without re-merging.
	if _, err := s.git(gitPath, "merge-base", "--is-ancestor", remoteRef, "HEAD"); err == nil {
		if err := s.push(gitPath, branch, url); err != nil {
			return rep, err
		}
		rep.Sent = countMissing(preIDs, theirIDs)
		return rep, nil
	}

	oursReg, err := s.readRegistry() // pre-merge decants: the ours-wins side
	if err != nil {
		return rep, err
	}
	theirsReg, err := s.remoteRegistry(gitPath, remoteRef)
	if err != nil {
		return rep, err
	}

	// --no-ff forces a real merge state even when HEAD is strictly behind:
	// --no-commit cannot stop a fast-forward, which would move HEAD before
	// the re-projection below runs and leave abortMerge nothing to abort if
	// a post-merge step failed. It also overrides user config like
	// merge.ff=only. The identity is pinned since git refuses to create a
	// merge commit without one.
	mergeArgs := append(append([]string{}, gitIdentity...),
		"merge", "--no-ff", "--no-commit", "--allow-unrelated-histories", remoteRef)
	if out, mergeErr := s.git(gitPath, mergeArgs...); mergeErr != nil {
		unmerged, _ := s.git(gitPath, "ls-files", "-u")
		if cErr := mergeConflictError(unmerged, out); cErr != nil {
			return rep, s.abortMerge(gitPath, cErr)
		}
		// Only registry.json conflicted; it is regenerated just below.
	}

	// Regenerate the registry from the unioned metas; union decants ours-wins.
	bottles, err := s.scanMetas()
	if err != nil {
		return rep, s.abortMerge(gitPath, err)
	}
	names, renames, err := projectNames(bottles)
	if err != nil {
		return rep, s.abortMerge(gitPath, err)
	}
	merged := newRegistry()
	merged.Decants = unionDecants(oursReg.Decants, theirsReg.Decants)
	if err := s.applyProjection(merged, names, renames); err != nil {
		return rep, s.abortMerge(gitPath, err)
	}
	if err := s.commitAll(gitPath, "sync: merge "+remoteRef); err != nil {
		return rep, s.abortMerge(gitPath, err)
	}
	if err := s.push(gitPath, branch, url); err != nil {
		return rep, err // merge is committed; the next sync pushes it
	}

	postIDs := make(map[string]bool, len(bottles))
	for _, b := range bottles {
		postIDs[b.ID] = true
	}
	rep.Received = countMissing(postIDs, preIDs)
	rep.Sent = countMissing(postIDs, theirIDs)
	rep.Renames = dedupeRenames(renames)
	return rep, nil
}

// SyncStatus compares HEAD against origin's last-fetched remote-tracking ref
// for the store's branch, from local refs only — never the network, never the
// lock. ok is false on any failure (substrate disabled, git absent, no repo,
// no remote, unborn HEAD): callers render nothing in that case, so the status
// can never make a remote-less store noisier. With a remote configured but no
// remote-tracking ref yet, the store was never pushed: every local commit
// counts as ahead, so the unsynced hint still shows.
func (s *Store) SyncStatus() (ahead, behind int, ok bool) {
	if !s.config.gitEnabled() {
		return 0, 0, false
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return 0, 0, false
	}
	if _, err := os.Stat(filepath.Join(s.root, ".git")); err != nil {
		return 0, 0, false
	}
	if _, hasOrigin := s.originURL(gitPath); !hasOrigin {
		return 0, 0, false
	}
	branch, err := s.currentBranch(gitPath)
	if err != nil {
		return 0, 0, false
	}
	remoteRef := "refs/remotes/origin/" + branch
	if _, err := s.git(gitPath, "rev-parse", "--verify", "--quiet", remoteRef); err != nil {
		// Never pushed: everything local is unsynced.
		out, err := s.git(gitPath, "rev-list", "--count", "HEAD")
		if err != nil {
			return 0, 0, false
		}
		n, err := strconv.Atoi(out)
		if err != nil {
			return 0, 0, false
		}
		return n, 0, true
	}
	out, err := s.git(gitPath, "rev-list", "--left-right", "--count", "HEAD..."+remoteRef)
	if err != nil {
		return 0, 0, false
	}
	left, right, found := strings.Cut(out, "\t")
	if !found {
		return 0, 0, false
	}
	a, errA := strconv.Atoi(strings.TrimSpace(left))
	b, errB := strconv.Atoi(strings.TrimSpace(right))
	if errA != nil || errB != nil {
		return 0, 0, false
	}
	return a, b, true
}

// setOrigin adds or replaces the origin remote.
func (s *Store) setOrigin(gitPath, url string) error {
	verb := "add"
	if _, ok := s.originURL(gitPath); ok {
		verb = "set-url"
	}
	if out, err := s.git(gitPath, "remote", verb, "origin", url); err != nil {
		return fmt.Errorf("cannot configure remote %s: %s", url, firstLine(out))
	}
	return nil
}

// commitAll stages everything and commits with the bottle identity. An empty
// stage is a no-op — unless a merge is being concluded, where the (possibly
// content-empty) merge commit must still be recorded.
func (s *Store) commitAll(gitPath, msg string) error {
	if out, err := s.git(gitPath, "add", "-A"); err != nil {
		return fmt.Errorf("git add failed: %s", firstLine(out))
	}
	_, mergeHeadErr := os.Stat(filepath.Join(s.root, ".git", "MERGE_HEAD"))
	if _, err := s.git(gitPath, "diff", "--cached", "--quiet"); err == nil && mergeHeadErr != nil {
		return nil // nothing staged, no merge to conclude
	}
	args := append(append([]string{}, gitIdentity...), "commit", "-q", "-m", msg)
	if out, err := s.git(gitPath, args...); err != nil {
		return fmt.Errorf("git commit failed: %s", firstLine(out))
	}
	return nil
}

// push publishes the store's branch, with upstream tracking for SyncStatus.
func (s *Store) push(gitPath, branch, url string) error {
	if out, err := s.git(gitPath, "push", "-q", "-u", "origin", branch); err != nil {
		return fmt.Errorf("push to %s failed: %s", url, firstLine(out))
	}
	return nil
}

// abortMerge backs an in-progress merge out so a failed sync leaves the
// worktree exactly as last committed; the original error wins over any
// abort noise.
func (s *Store) abortMerge(gitPath string, err error) error {
	s.git(gitPath, "merge", "--abort")
	return err
}

// mergeConflictError inspects the unmerged index after a failed merge. A
// registry.json conflict is fine (nil — the registry is regenerated from the
// metas); a conflict under store/ means one bottle id carries different
// content on both machines, which sync never auto-resolves; anything else is
// a generic merge failure.
func mergeConflictError(unmerged, mergeOut string) error {
	pathSet := map[string]bool{}
	for _, line := range strings.Split(unmerged, "\n") {
		if _, p, ok := strings.Cut(line, "\t"); ok {
			pathSet[strings.TrimSpace(p)] = true
		}
	}
	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return fmt.Errorf("merge failed: %s — sync aborted, store unchanged", firstLine(mergeOut))
	}
	registryOnly := true
	for _, p := range paths {
		if rest, ok := strings.CutPrefix(p, "store/"); ok {
			id, _, _ := strings.Cut(rest, "/")
			return fmt.Errorf("bottle %s has conflicting content on both machines — sync aborted, store unchanged", id)
		}
		if p != registryFile {
			registryOnly = false
		}
	}
	if !registryOnly {
		return fmt.Errorf("merge conflict outside the store layout — sync aborted, store unchanged: %s", firstLine(mergeOut))
	}
	return nil
}

// remoteRegistry reads the fetched ref's registry.json; a remote without one
// yet reads as empty.
func (s *Store) remoteRegistry(gitPath, ref string) (*registry, error) {
	out, err := s.git(gitPath, "show", ref+":"+registryFile)
	if err != nil {
		return newRegistry(), nil
	}
	reg := newRegistry()
	if err := json.Unmarshal([]byte(out), reg); err != nil {
		return nil, fmt.Errorf("remote %s is corrupt: %v", registryFile, err)
	}
	return reg, nil
}

// localBottleIDs lists the bottle ids present on disk (by meta.json).
func (s *Store) localBottleIDs() (map[string]bool, error) {
	paths, err := s.backend.List("store")
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, p := range paths {
		if dir, file, ok := strings.Cut(p, "/"); ok && file == "meta.json" {
			ids[dir] = true
		}
	}
	return ids, nil
}

// remoteBottleIDs lists the bottle ids in the fetched ref's store/ tree.
func (s *Store) remoteBottleIDs(gitPath, ref string) (map[string]bool, error) {
	out, err := s.git(gitPath, "ls-tree", "--name-only", ref, "store/")
	if err != nil {
		return nil, fmt.Errorf("cannot list the remote's bottles: %s", firstLine(out))
	}
	ids := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if id, ok := strings.CutPrefix(strings.TrimSpace(line), "store/"); ok && id != "" {
			ids[id] = true
		}
	}
	return ids, nil
}

// unionDecants merges two decants maps, ours winning on a shared session id.
func unionDecants(ours, theirs map[string]Decant) map[string]Decant {
	u := make(map[string]Decant, len(ours)+len(theirs))
	for id, d := range theirs {
		u[id] = d
	}
	for id, d := range ours {
		u[id] = d
	}
	return u
}

// countMissing counts ids in have that are absent from in.
func countMissing(have, in map[string]bool) int {
	n := 0
	for id := range have {
		if !in[id] {
			n++
		}
	}
	return n
}

// dedupeRenames reduces per-bottle rename records to one per (old, new) name
// pair, preserving order — the per-name view the CLI reports.
func dedupeRenames(renames []SyncRename) []SyncRename {
	seen := map[[2]string]bool{}
	var out []SyncRename
	for _, r := range renames {
		k := [2]string{r.OldName, r.NewName}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}
