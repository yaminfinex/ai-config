package webstate

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func row(key string, updated int64, writeID string, value any, deleted bool) Row {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return Row{Key: key, Value: raw, Updated: updated, WriteID: writeID, Deleted: deleted}
}

func TestComparatorMatchesTheSharedClientServerCorpus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "state-comparator.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus []struct {
		Left struct {
			Updated int64  `json:"updated"`
			WriteID string `json:"writeID"`
		} `json:"left"`
		Right struct {
			Updated int64  `json:"updated"`
			WriteID string `json:"writeID"`
		} `json:"right"`
		Winner string `json:"winner"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, test := range corpus {
		got := Compare(test.Left.Updated, test.Left.WriteID, test.Right.Updated, test.Right.WriteID)
		want := map[string]int{"left": 1, "equal": 0, "right": -1}[test.Winner]
		if got != want {
			t.Fatalf("Compare(%d, %q, %d, %q) = %d, want %d", test.Left.Updated, test.Left.WriteID, test.Right.Updated, test.Right.WriteID, got, want)
		}
	}
}

func TestReorderBatchCompetesAsOneWholeBatch(t *testing.T) {
	store, err := NewFileStore(t.TempDir(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	seed := []Row{
		row("a", 1, "seed", map[string]any{"order": 0}, false),
		row("b", 1, "seed", map[string]any{"order": 1}, false),
		row("c", 1, "seed", map[string]any{"order": 2}, false),
	}
	if accepted, _, err := store.Upsert("web-alice", "spaces", seed); err != nil || len(accepted) != 3 {
		t.Fatalf("seed accepted=%v err=%v", accepted, err)
	}

	winning := []Row{
		row("a", 10, "z-batch", map[string]any{"order": 2}, false),
		row("b", 10, "z-batch", map[string]any{"order": 0}, false),
		row("c", 10, "z-batch", map[string]any{"order": 1}, false),
	}
	losing := []Row{
		row("a", 10, "a-batch", map[string]any{"order": 1}, false),
		row("b", 10, "a-batch", map[string]any{"order": 2}, false),
		row("c", 10, "a-batch", map[string]any{"order": 0}, false),
	}
	if accepted, _, err := store.Upsert("web-alice", "spaces", winning); err != nil || !reflect.DeepEqual(accepted, []string{"a", "b", "c"}) {
		t.Fatalf("winning accepted=%v err=%v", accepted, err)
	}
	if accepted, _, err := store.Upsert("web-alice", "spaces", losing); err != nil || len(accepted) != 0 {
		t.Fatalf("losing accepted=%v err=%v", accepted, err)
	}
	rows, _, err := store.Since("web-alice", "spaces", 0)
	if err != nil {
		t.Fatal(err)
	}
	orders := map[string]float64{}
	for _, got := range rows {
		var value map[string]float64
		if err := json.Unmarshal(got.Value, &value); err != nil {
			t.Fatal(err)
		}
		orders[got.Key] = value["order"]
	}
	if !reflect.DeepEqual(orders, map[string]float64{"a": 2, "b": 0, "c": 1}) {
		t.Fatalf("orders=%v", orders)
	}
}

func TestThreeReplicasConvergeAcrossRandomSpaceInterleavings(t *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		random := rand.New(rand.NewSource(seed))
		store, err := NewFileStore(t.TempDir(), DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		clients := [2]map[string]Row{{}, {}}
		clock := int64(1)
		for step := 0; step < 100; step++ {
			client := random.Intn(2)
			key := []string{"main", "review", "triage", "debug"}[random.Intn(4)]
			clock++
			writeID := string(rune('a'+client)) + "-" + string(rune('a'+step%26))
			operation := random.Intn(4)
			value := map[string]any{"name": key, "order": random.Intn(4), "operation": operation}
			candidate := row(key, clock, writeID, value, operation == 3)
			clients[client][key] = candidate
			if _, _, err := store.Upsert("web-alice", "spaces", []Row{candidate}); err != nil {
				t.Fatal(err)
			}
			if random.Intn(3) == 0 {
				remote, _, err := store.Since("web-alice", "spaces", 0)
				if err != nil {
					t.Fatal(err)
				}
				mergeRows(clients[1-client], remote)
			}
		}
		answer, _, err := store.Since("web-alice", "spaces", 0)
		if err != nil {
			t.Fatal(err)
		}
		mergeRows(clients[0], answer)
		mergeRows(clients[1], answer)
		if !reflect.DeepEqual(clients[0], clients[1]) || !reflect.DeepEqual(clients[0], rowsByKey(answer)) {
			t.Fatalf("seed %d did not converge\nclient0=%v\nclient1=%v\nserver=%v", seed, clients[0], clients[1], rowsByKey(answer))
		}
	}
}

func mergeRows(target map[string]Row, incoming []Row) {
	for _, candidate := range incoming {
		current, ok := target[candidate.Key]
		if !ok || Compare(current.Updated, current.WriteID, candidate.Updated, candidate.WriteID) < 0 {
			target[candidate.Key] = candidate
		}
	}
}

func rowsByKey(rows []Row) map[string]Row {
	result := map[string]Row{}
	mergeRows(result, rows)
	return result
}

func TestFreshDevicesMergeTheDeterministicMainIDWithoutDuplicatingIt(t *testing.T) {
	store, err := NewFileStore(t.TempDir(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	first := row("main", 100, "device-a", map[string]any{"name": "main"}, false)
	second := row("main", 101, "device-b", map[string]any{"name": "primary"}, false)
	if _, _, err := store.Upsert("web-alice", "spaces", []Row{first}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Upsert("web-alice", "spaces", []Row{second}); err != nil {
		t.Fatal(err)
	}
	rows, _, err := store.Since("web-alice", "spaces", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Key != "main" || string(rows[0].Value) != `{"name":"primary"}` {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestSinceCursorRetainsTombstonesAndSecondNamespaceNeedsNoChanges(t *testing.T) {
	store, err := NewFileStore(t.TempDir(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	_, firstRev, err := store.Upsert("web-alice", "spaces", []Row{row("retained", 1, "live", map[string]any{"name": "retained"}, false)})
	if err != nil {
		t.Fatal(err)
	}
	_, tombstoneRev, err := store.Upsert("web-alice", "spaces", []Row{row("retained", 2, "closed", nil, true)})
	if err != nil {
		t.Fatal(err)
	}
	rows, gotRev, err := store.Since("web-alice", "spaces", firstRev)
	if err != nil || gotRev != tombstoneRev || len(rows) != 1 || !rows[0].Deleted {
		t.Fatalf("rows=%+v rev=%d err=%v", rows, gotRev, err)
	}
	if _, _, err := store.Upsert("web-alice", "notes", []Row{row("n1", 3, "note", map[string]any{"body": "hello"}, false)}); err != nil {
		t.Fatal(err)
	}
	notes, _, err := store.Since("web-alice", "notes", 0)
	if err != nil || len(notes) != 1 || notes[0].Key != "n1" {
		t.Fatalf("notes=%+v err=%v", notes, err)
	}
}

func TestFileStorePersistsAtomicallyAndEnforcesNamedBoundsWithoutEviction(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root, Limits{MaxValueBytes: 8, MaxRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Upsert("web-alice", "spaces", []Row{row("too-large", 1, "a", "123456789", false)}); !errors.Is(err, ErrValueTooLarge) || !strings.Contains(err.Error(), "too-large") || !strings.Contains(err.Error(), "8") {
		t.Fatalf("value bound error=%v", err)
	}
	if _, _, err := store.Upsert("web-alice", "spaces", []Row{row("a", 1, "a", 1, false), row("b", 1, "b", 2, true)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Upsert("web-alice", "spaces", []Row{row("c", 2, "c", 3, false)}); !errors.Is(err, ErrRowLimit) || !strings.Contains(err.Error(), "2") {
		t.Fatalf("row bound error=%v", err)
	}
	rows, _, err := store.Since("web-alice", "spaces", 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	if rows[0].Key != "a" || rows[1].Key != "b" || !rows[1].Deleted {
		t.Fatalf("rows=%+v", rows)
	}
	leftovers, err := filepath.Glob(filepath.Join(root, "web-alice", "*.tmp-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files=%v err=%v", leftovers, err)
	}
}
