package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var syncEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// syncBottle builds a projection-input fixture: id, name@version, a created
// offset from syncEpoch, and an optional parent bottle id.
func syncBottle(id, name string, version int, createdOffset time.Duration, parentID string) Bottle {
	m := Meta{Name: name, Version: version, Created: syncEpoch.Add(createdOffset)}
	if parentID != "" {
		m.Parent = &Parent{BottleID: parentID, DecantSessionID: "decant-" + parentID}
	}
	return Bottle{ID: id, Meta: m}
}

// syncFixtures is every projection scenario, shared with the determinism
// test so the convergence property is asserted across all of them.
var syncFixtures = map[string][]Bottle{
	"empty": nil,
	"disjoint": {
		syncBottle("aaaa0001", "alpha", 1, 0, ""),
		syncBottle("bbbb0001", "beta", 1, time.Minute, ""),
	},
	"already-synced": {
		syncBottle("id000001", "auth-expert", 1, 0, ""),
		syncBottle("id000002", "auth-expert", 2, time.Hour, "id000001"),
	},
	"independent-create": {
		syncBottle("aaaa1111", "auth-expert", 1, 0, ""),
		syncBottle("bbbb1111", "auth-expert", 1, time.Hour, ""),
	},
	"divergent-rebottle": {
		syncBottle("shared01", "auth-expert", 1, 0, ""),
		syncBottle("shared02", "auth-expert", 2, time.Hour, "shared01"),
		syncBottle("aaaa3333", "auth-expert", 3, 2*time.Hour, "shared02"),
		syncBottle("bbbb3333", "auth-expert", 3, 3*time.Hour, "shared02"),
		syncBottle("bbbb4444", "auth-expert", 4, 4*time.Hour, "bbbb3333"),
	},
	"created-tie": {
		syncBottle("aaaa1111", "auth-expert", 1, 0, ""),
		syncBottle("bbbb1111", "auth-expert", 1, 0, ""),
	},
	"suffix-exhaustion": {
		syncBottle("aaaa1111", "auth-expert", 1, 0, ""),
		syncBottle("bbbb1111", "auth-expert", 1, time.Hour, ""),
		syncBottle("cccc1111", "auth-expert-2", 1, 0, ""),
	},
	"rm-vs-rebottle": {
		syncBottle("shared01", "auth-expert", 1, 0, ""),
		syncBottle("aaaa3333", "auth-expert", 3, 2*time.Hour, "deadbeef"),
	},
	"stacked-both-sides": {
		syncBottle("aaaa3333", "auth-expert", 3, 0, ""),
		syncBottle("aaaa4444", "auth-expert", 4, time.Hour, "aaaa3333"),
		syncBottle("bbbb3333", "auth-expert", 3, 2*time.Hour, ""),
		syncBottle("bbbb4444", "auth-expert", 4, 3*time.Hour, "bbbb3333"),
	},
}

func mustProject(t *testing.T, bottles []Bottle) (map[string][]versionEntry, []SyncRename) {
	t.Helper()
	names, renames, err := projectNames(bottles)
	if err != nil {
		t.Fatalf("projectNames: %v", err)
	}
	return names, renames
}

func TestProjectDisjointUnion(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["disjoint"])
	want := map[string][]versionEntry{
		"alpha": {{Version: 1, BottleID: "aaaa0001"}},
		"beta":  {{Version: 1, BottleID: "bbbb0001"}},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	if len(renames) != 0 {
		t.Errorf("renames = %v, want none", renames)
	}
}

func TestProjectAlreadySynced(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["already-synced"])
	want := map[string][]versionEntry{
		"auth-expert": {{Version: 1, BottleID: "id000001"}, {Version: 2, BottleID: "id000002"}},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	if len(renames) != 0 {
		t.Errorf("renames = %v, want none", renames)
	}
}

func TestProjectIndependentCreateCollision(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["independent-create"])
	want := map[string][]versionEntry{
		"auth-expert":   {{Version: 1, BottleID: "aaaa1111"}},
		"auth-expert-2": {{Version: 1, BottleID: "bbbb1111"}},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	wantRenames := []SyncRename{{BottleID: "bbbb1111", OldName: "auth-expert", NewName: "auth-expert-2"}}
	if !reflect.DeepEqual(renames, wantRenames) {
		t.Errorf("renames = %v, want %v", renames, wantRenames)
	}
}

func TestProjectDivergentRebottleCollision(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["divergent-rebottle"])
	want := map[string][]versionEntry{
		"auth-expert": {
			{Version: 1, BottleID: "shared01"},
			{Version: 2, BottleID: "shared02"},
			{Version: 3, BottleID: "aaaa3333"},
		},
		"auth-expert-2": {
			{Version: 3, BottleID: "bbbb3333"},
			{Version: 4, BottleID: "bbbb4444"},
		},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	wantRenames := []SyncRename{
		{BottleID: "bbbb3333", OldName: "auth-expert", NewName: "auth-expert-2"},
		{BottleID: "bbbb4444", OldName: "auth-expert", NewName: "auth-expert-2"},
	}
	if !reflect.DeepEqual(renames, wantRenames) {
		t.Errorf("renames = %v, want %v", renames, wantRenames)
	}
}

func TestProjectCreatedTieSmallerIDWins(t *testing.T) {
	names, _ := mustProject(t, syncFixtures["created-tie"])
	if got := names["auth-expert"]; len(got) != 1 || got[0].BottleID != "aaaa1111" {
		t.Errorf("auth-expert = %v, want kept by smaller id aaaa1111", got)
	}
	if got := names["auth-expert-2"]; len(got) != 1 || got[0].BottleID != "bbbb1111" {
		t.Errorf("auth-expert-2 = %v, want loser bbbb1111", got)
	}
}

func TestProjectSuffixExhaustion(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["suffix-exhaustion"])
	if got := names["auth-expert-3"]; len(got) != 1 || got[0].BottleID != "bbbb1111" {
		t.Errorf("auth-expert-3 = %v, want loser bbbb1111 (auth-expert-2 is taken)", got)
	}
	if got := names["auth-expert-2"]; len(got) != 1 || got[0].BottleID != "cccc1111" {
		t.Errorf("auth-expert-2 = %v, want the pre-existing cccc1111 untouched", got)
	}
	wantRenames := []SyncRename{{BottleID: "bbbb1111", OldName: "auth-expert", NewName: "auth-expert-3"}}
	if !reflect.DeepEqual(renames, wantRenames) {
		t.Errorf("renames = %v, want %v", renames, wantRenames)
	}
}

func TestProjectDanglingParent(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["rm-vs-rebottle"])
	want := map[string][]versionEntry{
		"auth-expert": {{Version: 1, BottleID: "shared01"}, {Version: 3, BottleID: "aaaa3333"}},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v (dangling parent must not drop the child)", names, want)
	}
	if len(renames) != 0 {
		t.Errorf("renames = %v, want none", renames)
	}
}

// TestProjectStackedBothSides: a collision at @3 whose resolution moves the
// loser's stacked @4 too, dissolving the @4 collision in the same pass.
func TestProjectStackedBothSides(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["stacked-both-sides"])
	want := map[string][]versionEntry{
		"auth-expert": {
			{Version: 3, BottleID: "aaaa3333"},
			{Version: 4, BottleID: "aaaa4444"},
		},
		"auth-expert-2": {
			{Version: 3, BottleID: "bbbb3333"},
			{Version: 4, BottleID: "bbbb4444"},
		},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	if len(renames) != 2 {
		t.Errorf("renames = %v, want exactly the two bbbb bottles", renames)
	}
}

func TestProjectEmpty(t *testing.T) {
	names, renames := mustProject(t, syncFixtures["empty"])
	if len(names) != 0 {
		t.Errorf("names = %v, want empty", names)
	}
	if len(renames) != 0 {
		t.Errorf("renames = %v, want none", renames)
	}
}

// TestProjectDeterminism: project(A ∪ B) == project(B ∪ A), byte-identical
// after JSON marshal, across every fixture.
func TestProjectDeterminism(t *testing.T) {
	for label, bottles := range syncFixtures {
		t.Run(label, func(t *testing.T) {
			forward := append([]Bottle(nil), bottles...)
			reversed := make([]Bottle, len(bottles))
			for i, b := range bottles {
				reversed[len(bottles)-1-i] = b
			}
			fNames, fRenames := mustProject(t, forward)
			rNames, rRenames := mustProject(t, reversed)

			fJSON, err := json.Marshal(fNames)
			if err != nil {
				t.Fatal(err)
			}
			rJSON, err := json.Marshal(rNames)
			if err != nil {
				t.Fatal(err)
			}
			if string(fJSON) != string(rJSON) {
				t.Errorf("projection depends on input order:\nforward:  %s\nreversed: %s", fJSON, rJSON)
			}
			if !reflect.DeepEqual(fRenames, rRenames) {
				t.Errorf("renames depend on input order:\nforward:  %v\nreversed: %v", fRenames, rRenames)
			}
		})
	}
}

// TestScanProjectApply exercises the store-side path: a real bottle plus a
// hand-planted colliding bottle dir (as a git merge union would leave), then
// scan → project → apply, asserting the rewritten meta and on-disk registry.
func TestScanProjectApply(t *testing.T) {
	s, _ := newStore(t)
	winner, err := s.Create(mkCreate("auth-expert", "sess-1"))
	if err != nil {
		t.Fatal(err)
	}

	// Plant a colliding auth-expert@1 with a newer Created and an id that
	// sorts after the real one regardless of its random value.
	loserID := "zzzzzzzz"
	loserMeta := Meta{
		Name:    "auth-expert",
		Version: 1,
		Created: winner.Meta.Created.Add(time.Hour),
		Source:  Source{SessionID: "sess-2", Harness: "claude", CWD: "/tmp/proj"},
		Parent:  &Parent{BottleID: winner.ID, DecantSessionID: "decant-x"},
	}
	if err := s.writeMeta(loserID, loserMeta); err != nil {
		t.Fatal(err)
	}
	if err := s.backend.Write(bottlePath(loserID, "transcript.jsonl"), []byte("{}\n")); err != nil {
		t.Fatal(err)
	}

	bottles, err := s.scanMetas()
	if err != nil {
		t.Fatalf("scanMetas: %v", err)
	}
	if len(bottles) != 2 {
		t.Fatalf("scanMetas found %d bottles, want 2", len(bottles))
	}
	names, renames, err := projectNames(bottles)
	if err != nil {
		t.Fatalf("projectNames: %v", err)
	}

	unlock, err := s.lockExclusive()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := s.readRegistry()
	if err != nil {
		unlock()
		t.Fatal(err)
	}
	err = s.applyProjection(reg, names, renames)
	unlock()
	if err != nil {
		t.Fatalf("applyProjection: %v", err)
	}

	// Loser meta rewritten via the rename idiom; parent pointer untouched.
	loser, err := s.Get(loserID)
	if err != nil {
		t.Fatal(err)
	}
	if loser.Meta.Name != "auth-expert-2" {
		t.Errorf("loser name = %q, want auth-expert-2", loser.Meta.Name)
	}
	if len(loser.Meta.PreviousNames) != 1 || loser.Meta.PreviousNames[0] != "auth-expert" {
		t.Errorf("loser previous names = %v, want [auth-expert]", loser.Meta.PreviousNames)
	}
	if loser.Meta.Parent == nil || loser.Meta.Parent.BottleID != winner.ID {
		t.Errorf("loser parent = %+v, want untouched pointer to %s", loser.Meta.Parent, winner.ID)
	}

	// Winner meta untouched.
	w, err := s.Get(winner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Meta.Name != "auth-expert" || len(w.Meta.PreviousNames) != 0 {
		t.Errorf("winner meta changed: name=%q previous=%v", w.Meta.Name, w.Meta.PreviousNames)
	}

	// On-disk registry reflects the projection.
	raw, err := os.ReadFile(filepath.Join(s.Root(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Names map[string][]struct {
			Version  int    `json:"version"`
			BottleID string `json:"bottle_id"`
		} `json:"names"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if e := got.Names["auth-expert"]; len(e) != 1 || e[0].BottleID != winner.ID || e[0].Version != 1 {
		t.Errorf("registry auth-expert = %+v, want [{1 %s}]", e, winner.ID)
	}
	if e := got.Names["auth-expert-2"]; len(e) != 1 || e[0].BottleID != loserID || e[0].Version != 1 {
		t.Errorf("registry auth-expert-2 = %+v, want [{1 %s}]", e, loserID)
	}
}
