package store

import (
	"fmt"
	"sort"
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
