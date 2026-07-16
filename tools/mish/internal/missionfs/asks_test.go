package missionfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAskID = "ask-019b2d6e-7c18-7f65-9d8d-4db7efc3b4ec"

func validTestAsk(stamp string) AskEntity {
	return AskEntity{Schema: AskSchema, ID: testAskID, Kind: "ask", State: "open", Outcome: nil, Asker: "vile", AddressedTo: "owner", CreatedAt: stamp, UpdatedAt: stamp, Expects: "decide", Anchor: TypedRef{Type: "task", Ref: "TASK-41"}, Links: []TypedRef{}, Members: []string{"vile", "owner"}, Framing: Framing{Context: "context", Question: "choose?", SubDecisions: []string{}, Options: []DecisionOption{{ID: "a", Label: "A", Cost: "c", Risk: "r", BlastRadius: "b"}, {ID: "b", Label: "B", Cost: "c", Risk: "r", BlastRadius: "b"}}, Recommendation: &Recommendation{Choice: "a", Reason: "best"}, DoNothing: "blocked"}, Replies: []Reply{}, RulingTrail: []RulingEntry{}, Traces: []Trace{}}
}

func TestAsksCreateRetryAndConflictingID(t *testing.T) {
	dir := t.TempDir()
	stamp := "2026-07-16T01:00:00Z"
	first := &AskDocument{Entity: validTestAsk(stamp)}
	if err := WriteNewAsk(dir, first); err != nil {
		t.Fatal(err)
	}
	retry := &AskDocument{Entity: validTestAsk("2026-07-16T02:00:00Z")}
	if err := WriteNewAsk(dir, retry); err != nil {
		t.Fatalf("equivalent retry: %v", err)
	}
	if retry.Entity.CreatedAt != stamp {
		t.Fatalf("retry did not return stored entity: %+v", retry.Entity)
	}
	conflict := validTestAsk(stamp)
	conflict.Expects = "reply"
	err := WriteNewAsk(dir, &AskDocument{Entity: conflict})
	f, ok := err.(*AskFailure)
	if !ok || f.Kind != "entity_exists" {
		t.Fatalf("conflict error = %#v", err)
	}
}

func TestMissingEntityMutationDoesNotInitializeHistoricalBoard(t *testing.T) {
	dir := t.TempDir()
	_, err := MutateAsk(dir, testAskID, "x", time.Now(), func(*AskDocument, string) *AskFailure { return nil })
	if err == nil {
		t.Fatal("missing mutation succeeded")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "asks")); !os.IsNotExist(statErr) {
		t.Fatalf("failed mutation created asks board: %v", statErr)
	}
}

func TestMutationPreservesUnknownKeysAndMarkdownBody(t *testing.T) {
	dir := t.TempDir()
	stamp := "2026-07-16T01:00:00Z"
	if err := WriteNewAsk(dir, &AskDocument{Entity: validTestAsk(stamp), Body: []byte("# Durable body\n\nKeep me.\n")}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "asks", "entities", testAskID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "framing:\n", "future_top: keep\nframing:\n    future_nested: nested\n", 1))
	data = []byte(strings.Replace(string(data), "anchor:\n", "anchor:\n    future_anchor: anchored\n", 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := MutateAsk(dir, testAskID, stamp, time.Date(2026, 7, 16, 1, 0, 0, 0, time.UTC), func(doc *AskDocument, at string) *AskFailure {
		AppendLink(doc, TypedRef{Type: "artifact", Ref: "artifacts/design.md"})
		SetAnchor(doc, TypedRef{Type: "artifact", Ref: "artifacts/design.md"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Entity.UpdatedAt != "2026-07-16T01:00:00.000000001Z" {
		t.Fatalf("logical timestamp = %q", doc.Entity.UpdatedAt)
	}
	after := string(mustRead(t, path))
	for _, want := range []string{"future_top: keep", "future_nested: nested", "future_anchor: anchored", "# Durable body\n\nKeep me.\n"} {
		if !strings.Contains(after, want) {
			t.Fatalf("rewrite lost %q:\n%s", want, after)
		}
	}
}

func TestRefusedMutationLeavesPriorFileByteExact(t *testing.T) {
	dir := t.TempDir()
	stamp := "2026-07-16T01:00:00Z"
	if err := WriteNewAsk(dir, &AskDocument{Entity: validTestAsk(stamp)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "asks", "entities", testAskID+".md")
	before := mustRead(t, path)
	_, err := MutateAsk(dir, testAskID, stamp, time.Now(), func(*AskDocument, string) *AskFailure {
		return &AskFailure{Kind: "invalid_transition", Message: "no", Remedy: "none"}
	})
	if err == nil {
		t.Fatal("refused mutation succeeded")
	}
	after := mustRead(t, path)
	if string(after) != string(before) {
		t.Fatal("refused mutation changed prior file")
	}
}

func TestSettlementSnapshotPreservesUnknownOptionKeys(t *testing.T) {
	dir := t.TempDir()
	stamp := "2026-07-16T01:00:00Z"
	if err := WriteNewAsk(dir, &AskDocument{Entity: validTestAsk(stamp)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "asks", "entities", testAskID+".md")
	data := string(mustRead(t, path))
	data = strings.Replace(data, "blast_radius: b", "blast_radius: b\n          future_option: keep", 1)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := MutateAsk(dir, testAskID, stamp, time.Date(2026, 7, 16, 1, 0, 1, 0, time.UTC), func(doc *AskDocument, at string) *AskFailure {
		AppendSettlementRuling(doc, RulingEntry{Question: doc.Entity.Framing.Question, OptionsAsPresented: doc.Entity.Framing.Options, Choice: "a", Prose: "settled", Actor: "owner", At: at})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	after := string(mustRead(t, path))
	if strings.Count(after, "future_option: keep") != 2 {
		t.Fatalf("unknown option was not copied into snapshot:\n%s", after)
	}
}

func TestConcurrentCASAllowsExactlyOneWriter(t *testing.T) {
	dir := t.TempDir()
	stamp := "2026-07-16T01:00:00Z"
	if err := WriteNewAsk(dir, &AskDocument{Entity: validTestAsk(stamp)}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := MutateAsk(dir, testAskID, stamp, time.Date(2026, 7, 16, 1, 0, 0, 0, time.UTC), func(doc *AskDocument, at string) *AskFailure {
				AppendReply(doc, Reply{ID: "reply-019b2d84-9e35-7c23-9a76-0ed69cf67d42", Actor: "owner", At: at, Prose: string(rune('a' + i))})
				return nil
			})
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	success, stale := 0, 0
	for err := range results {
		if err == nil {
			success++
			continue
		}
		if f, ok := err.(*AskFailure); ok && f.Kind == "stale_entity" {
			stale++
			continue
		}
		t.Fatalf("unexpected race result: %v", err)
	}
	if success != 1 || stale != 1 {
		t.Fatalf("success/stale = %d/%d", success, stale)
	}
	doc, err := ReadAsk(dir, testAskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Entity.Replies) != 1 {
		t.Fatalf("replies = %d", len(doc.Entity.Replies))
	}
}

func TestTwoProcessCASAllowsExactlyOneWriter(t *testing.T) {
	dir := t.TempDir()
	stamp := "2026-07-16T01:00:00Z"
	if err := WriteNewAsk(dir, &AskDocument{Entity: validTestAsk(stamp)}); err != nil {
		t.Fatal(err)
	}
	commands := make([]*exec.Cmd, 2)
	for i := range commands {
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestAskCASProcessHelper$")
		commands[i].Env = append(os.Environ(), "MISH_ASK_CAS_HELPER=1", "MISH_ASK_CAS_DIR="+dir, "MISH_ASK_CAS_PROSE="+string(rune('a'+i)))
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	success, stale := 0, 0
	for _, cmd := range commands {
		err := cmd.Wait()
		if err == nil {
			success++
			continue
		}
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 3 {
			stale++
			continue
		}
		t.Fatalf("helper failed: %v", err)
	}
	if success != 1 || stale != 1 {
		t.Fatalf("process success/stale = %d/%d", success, stale)
	}
}

func TestAskCASProcessHelper(t *testing.T) {
	if os.Getenv("MISH_ASK_CAS_HELPER") != "1" {
		return
	}
	_, err := MutateAsk(os.Getenv("MISH_ASK_CAS_DIR"), testAskID, "2026-07-16T01:00:00Z", time.Date(2026, 7, 16, 1, 0, 0, 0, time.UTC), func(doc *AskDocument, at string) *AskFailure {
		AppendReply(doc, Reply{ID: "reply-019b2d84-9e35-7c23-9a76-0ed69cf67d42", Actor: "owner", At: at, Prose: os.Getenv("MISH_ASK_CAS_PROSE")})
		return nil
	})
	if err == nil {
		return
	}
	if f, ok := err.(*AskFailure); ok && f.Kind == "stale_entity" {
		os.Exit(3)
	}
	t.Fatal(err)
}

func TestStatusProjectionKeepsReadableInvalidAndFutureEntities(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAsksScaffold(dir); err != nil {
		t.Fatal(err)
	}
	e := validTestAsk("2026-07-16T01:00:00Z")
	e.Framing.Options = nil
	if err := writeAsk(filepath.Join(dir, "asks", "entities", e.ID+".md"), e, nil, nil); err != nil {
		t.Fatal(err)
	}
	future := e
	future.ID = "ask-019b2d6e-7c18-7f65-9d8d-4db7efc3b4ed"
	future.Schema = "mish.ask/v2"
	if err := writeAsk(filepath.Join(dir, "asks", "entities", future.ID+".md"), future, nil, nil); err != nil {
		t.Fatal(err)
	}
	scan := ScanAsks(dir)
	if len(scan.Entities) != 2 || len(scan.Warnings) != 2 {
		t.Fatalf("scan = %+v", scan)
	}
	if _, err := MutateAsk(dir, future.ID, future.UpdatedAt, time.Now(), func(*AskDocument, string) *AskFailure { return nil }); err == nil {
		t.Fatal("future schema mutation succeeded")
	} else if f := err.(*AskFailure); f.Kind != "unsupported_schema_write" {
		t.Fatalf("future mutation = %+v", f)
	}
}

func TestConfigOnlyAsksBoardIsEmptyWithoutWarning(t *testing.T) {
	dir := t.TempDir()
	asks := filepath.Join(dir, "asks")
	if err := os.MkdirAll(asks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(asks, "config.yml"), []byte("schema: mish.asks/v1\nstates: [open, closed]\noutcomes: [settled, no-action, superseded]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scan := ScanAsks(dir)
	if !scan.Available || len(scan.Entities) != 0 || len(scan.Warnings) != 0 {
		t.Fatalf("scan=%+v", scan)
	}
}

func TestKnownMalformedLifecycleInvariantsWarn(t *testing.T) {
	stamp := "2026-07-16T01:00:00Z"
	cases := map[string]func(*AskEntity){
		"nil links":                       func(e *AskEntity) { e.Links = nil },
		"settled ask without trail":       func(e *AskEntity) { out := "settled"; e.State = "closed"; e.Outcome = &out },
		"open ruling without trail":       func(e *AskEntity) { e.Kind = "ruling" },
		"closed ruling without trail":     func(e *AskEntity) { out := "no-action"; e.Kind = "ruling"; e.State = "closed"; e.Outcome = &out },
		"closed nonsettled without trace": func(e *AskEntity) { out := "no-action"; e.State = "closed"; e.Outcome = &out },
		"closure outcome mismatch": func(e *AskEntity) {
			out := "no-action"
			e.State = "closed"
			e.Outcome = &out
			e.Traces = []Trace{{Action: "close", Actor: "owner", At: stamp, Outcome: "superseded", Reason: "old"}}
		},
		"widen member absent": func(e *AskEntity) {
			e.Traces = []Trace{{Action: "widen-membership", Actor: "owner", At: stamp, Member: "other"}}
		},
		"bad blocking time": func(e *AskEntity) { e.Blocking = &Blocking{Fact: "f", Actor: "vile", At: "bad"} },
		"bad withdrawal trace": func(e *AskEntity) {
			out := "superseded"
			e.State = "closed"
			e.Outcome = &out
			e.Traces = []Trace{{Action: "withdraw", Actor: "vile", At: stamp}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := validTestAsk(stamp)
			mutate(&e)
			if failure := ValidateAsk(e, "owner", ""); failure == nil {
				t.Fatalf("malformed entity accepted: %+v", e)
			}
		})
	}
}

func TestStatusProjectionWarnsOnDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAsksScaffold(dir); err != nil {
		t.Fatal(err)
	}
	e := validTestAsk("2026-07-16T01:00:00Z")
	for _, name := range []string{e.ID + ".md", "ask-019b2d6e-7c18-7f65-9d8d-4db7efc3b4ed.md"} {
		if err := writeAsk(filepath.Join(dir, "asks", "entities", name), e, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	scan := ScanAsks(dir)
	joined := strings.Join(scan.Warnings, "\n")
	if !strings.Contains(joined, "duplicate ask ID") || !strings.Contains(joined, "filename/id mismatch") {
		t.Fatalf("warnings = %v", scan.Warnings)
	}
}

func TestStatusProjectionSortsCreatedAtChronologically(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAsksScaffold(dir); err != nil {
		t.Fatal(err)
	}
	later := validTestAsk("2026-07-16T01:00:00.1Z")
	earlier := validTestAsk("2026-07-16T01:00:00Z")
	earlier.ID = "ask-019b2d6e-7c18-7f65-9d8d-4db7efc3b4ed"
	for _, e := range []AskEntity{later, earlier} {
		if err := writeAsk(filepath.Join(dir, "asks", "entities", e.ID+".md"), e, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	scan := ScanAsks(dir)
	if len(scan.Entities) != 2 || scan.Entities[0].ID != earlier.ID {
		t.Fatalf("order=%+v", scan.Entities)
	}
}

func TestMissingRequiredRawFieldWarnsButEntityRemainsVisible(t *testing.T) {
	dir := t.TempDir()
	stamp := "2026-07-16T01:00:00Z"
	if err := WriteNewAsk(dir, &AskDocument{Entity: validTestAsk(stamp)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "asks", "entities", testAskID+".md")
	data := string(mustRead(t, path))
	data = strings.Replace(data, "outcome: null\n", "", 1)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	scan := ScanAsks(dir, "owner")
	if len(scan.Entities) != 1 || len(scan.Warnings) != 1 || !strings.Contains(scan.Warnings[0], "missing required field outcome") {
		t.Fatalf("scan=%+v", scan)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
