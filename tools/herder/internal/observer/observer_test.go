package observer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/sessionvitals"
	"github.com/fsnotify/fsnotify"
)

const (
	claudeID = "73100000-0000-4000-8000-000000000731"
	codexID  = "73200000-0000-4000-8000-000000000732"
)

// claudeRecord is one complete assistant line of about size bytes.
func claudeRecord(model string, used int64, size int) string {
	head := fmt.Sprintf(`{"type":"assistant","isSidechain":false,"message":{"model":%q,"usage":{"input_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1},"content":[{"type":"text","text":"`, model, used)
	tail := `"}]}}` + "\n"
	pad := size - len(head) - len(tail)
	if pad < 0 {
		pad = 0
	}
	return head + strings.Repeat("x", pad) + tail
}

func codexRecords(model string, used int64, size int) string {
	turn := fmt.Sprintf(`{"type":"turn_context","payload":{"model":%q}}`+"\n", model)
	head := fmt.Sprintf(`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":%d,"cached_input_tokens":0,"output_tokens":1},"model_context_window":258400},"pad":"`, used)
	tail := `"}}` + "\n"
	pad := size - len(turn) - len(head) - len(tail)
	if pad < 0 {
		pad = 0
	}
	return turn + head + strings.Repeat("y", pad) + tail
}

type fixture struct {
	home   string
	claude string
	codex  string
	rows   []hcomidentity.Row
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	home := t.TempDir()
	claude := filepath.Join(home, ".claude", "projects", "-invented-violet", claudeID+".jsonl")
	codex := filepath.Join(home, ".codex", "sessions", "2026", "09", "10", "rollout-invented-"+codexID+".jsonl")
	for _, dir := range []string{filepath.Dir(claude), filepath.Dir(codex)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return fixture{home: home, claude: claude, codex: codex, rows: []hcomidentity.Row{
		{Name: "impl-vamo", Tool: "claude", Directory: "/invented/violet", SessionID: claudeID},
		{Name: "impl-teka", Tool: "codex", SessionID: codexID, TranscriptPath: codex},
	}}
}

func appendFile(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func newObserver(f fixture, clock *fakeClock, watcher WatcherFactory) *Observer {
	rows := f.rows
	return New(Options{
		Roster:  func() ([]hcomidentity.Row, error) { return rows, nil },
		Now:     clock.Now,
		Watcher: watcher,
		Home:    f.home,
		Poll:    time.Hour,
		Sweep:   time.Hour,
	})
}

func assertParity(t *testing.T, o *Observer, row hcomidentity.Row) {
	t.Helper()
	t.Setenv("HOME", o.opts.Home)
	direct, err := sessionvitals.ReadDirect(row)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := o.Lookup(row)
	if !ok {
		t.Fatalf("observer miss for %s", row.Name)
	}
	if !reflect.DeepEqual(got.Vitals, direct.Vitals) {
		t.Fatalf("parity: observer %+v / %+v vs direct %+v / %+v", got.Vitals.Model, got.Vitals.ContextUsage, direct.Vitals.Model, direct.Vitals.ContextUsage)
	}
}

// Reddens: re-scanning from EOF or from zero on every change. Every step
// reads exactly the appended bytes and the offset only advances.
func TestAdvanceReadsOnlyAppendedBytesAndMatchesDirect(t *testing.T) {
	f := newFixture(t)
	appendFile(t, f.claude, claudeRecord("invented-claude-0", 100, 512))
	appendFile(t, f.codex, codexRecords("invented-codex-0", 100, 512))
	o := newObserver(f, &fakeClock{now: time.Unix(1_700_000_000, 0)}, nil)
	o.sync(f.rows)
	for step := 1; step <= 4; step++ {
		for _, row := range f.rows {
			key := KeyFor(row)
			before, _ := o.lookupState(key)
			if before.phase != PhaseTailing {
				t.Fatalf("%s phase = %s", row.Name, before.phase)
			}
			var chunk string
			path := f.claude
			if row.Tool == "codex" {
				chunk = codexRecords(fmt.Sprintf("invented-codex-%d", step), int64(1000*step), 64<<10)
				path = f.codex
			} else {
				chunk = claudeRecord(fmt.Sprintf("invented-claude-%d", step), int64(1000*step), 64<<10)
			}
			appendFile(t, path, chunk)
			o.mu.Lock()
			read := o.advance(o.sessions[key])
			o.mu.Unlock()
			if read != int64(len(chunk)) {
				t.Fatalf("step %d %s: read %d bytes, appended %d", step, row.Name, read, len(chunk))
			}
			after, _ := o.lookupState(key)
			if after.offset != before.offset+int64(len(chunk)) || after.vitals.ContextUsage.UsedTokens != int64(1000*step) {
				t.Fatalf("step %d %s: offset %d→%d, vitals %+v", step, row.Name, before.offset, after.offset, after.vitals.ContextUsage)
			}
			assertParity(t, o, row)
		}
	}
}

// Reddens: offset past EOF kept, stale vitals after a shrink.
func TestTruncationReseeds(t *testing.T) {
	f := newFixture(t)
	appendFile(t, f.claude, claudeRecord("invented-claude-a", 100, 4096))
	appendFile(t, f.claude, claudeRecord("invented-claude-b", 200, 4096))
	o := newObserver(f, &fakeClock{now: time.Unix(1_700_000_000, 0)}, nil)
	o.sync(f.rows)
	if err := os.WriteFile(f.claude, []byte(claudeRecord("invented-claude-c", 300, 1024)), 0o644); err != nil {
		t.Fatal(err)
	}
	o.sweep()
	got, _ := o.lookupState(KeyFor(f.rows[0]))
	if got.phase != PhaseTailing || got.vitals.Model != "invented-claude-c" || got.offset != 1024 {
		t.Fatalf("after truncation: %+v", got)
	}
	if !strings.Contains(got.err, "truncated") && got.err != "" {
		t.Fatalf("err = %q", got.err)
	}
	assertParity(t, o, f.rows[0])
}

// Reddens: create events ignored (polling only). Sweep and poll are an hour.
func TestAwaitingFileBecomesTailingOnCreate(t *testing.T) {
	f := newFixture(t)
	o := newObserver(f, &fakeClock{now: time.Unix(1_700_000_000, 0)}, fsnotify.NewWatcher)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o.Run(ctx)
	key := KeyFor(f.rows[0])
	if got, _ := o.lookupState(key); got.phase != PhaseAwaitingFile || !got.watched {
		t.Fatalf("before create: %+v", got)
	}
	appendFile(t, f.claude, claudeRecord("invented-claude-new", 4200, 2048))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := o.lookupState(key); got.phase == PhaseTailing && got.vitals.Model == "invented-claude-new" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, _ := o.lookupState(key)
	t.Fatalf("still %+v after create", got)
}

// Reddens: a name-keyed table (resume would overwrite instead of supersede).
func TestRosterSessionChangeSupersedes(t *testing.T) {
	f := newFixture(t)
	appendFile(t, f.claude, claudeRecord("invented-claude-old", 1, 1024))
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	o := newObserver(f, clock, nil)
	o.sync(f.rows)
	resumed := "73100000-0000-4000-8000-000000000999"
	rows := []hcomidentity.Row{{Name: "impl-vamo", Tool: "claude", Directory: "/invented/violet", SessionID: resumed}, f.rows[1]}
	o.sync(rows)
	old, _ := o.lookupState(KeyFor(f.rows[0]))
	fresh, _ := o.lookupState(KeyFor(rows[0]))
	if old.phase != PhaseEnded || fresh.phase != PhaseAwaitingFile {
		t.Fatalf("old %s fresh %s", old.phase, fresh.phase)
	}
	if _, ok := o.Lookup(f.rows[0]); !ok {
		t.Fatal("ended session within TTL must still answer its last vitals")
	}
}

// Reddens: forgetting immediately, or never dropping.
func TestGoneNameEndedRetainedThenDropped(t *testing.T) {
	f := newFixture(t)
	appendFile(t, f.codex, codexRecords("invented-codex", 5, 1024))
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	o := newObserver(f, clock, nil)
	o.sync(f.rows)
	o.sync(f.rows[:1])
	if got, ok := o.lookupState(KeyFor(f.rows[1])); !ok || got.phase != PhaseEnded {
		t.Fatalf("after leaving roster: %+v %v", got, ok)
	}
	if _, ok := o.Lookup(f.rows[1]); !ok {
		t.Fatal("ended session must still hit")
	}
	clock.now = clock.now.Add(DefaultTTL - time.Second)
	o.sync(f.rows[:1])
	if _, ok := o.lookupState(KeyFor(f.rows[1])); !ok {
		t.Fatal("dropped before TTL")
	}
	clock.now = clock.now.Add(2 * time.Second)
	o.sync(f.rows[:1])
	if _, ok := o.lookupState(KeyFor(f.rows[1])); ok {
		t.Fatal("not dropped after TTL")
	}
}

// Reddens: an error on the 65th directory, or a frozen session above it.
func TestWatchCeilingDegradesToSweep(t *testing.T) {
	f := newFixture(t)
	rows := make([]hcomidentity.Row, 0, MaxWatchDirectories+2)
	paths := make([]string, 0, cap(rows))
	for i := 0; i < cap(rows); i++ {
		id := fmt.Sprintf("73100000-0000-4000-8000-%012d", i)
		dir := filepath.Join(f.home, ".claude", "projects", fmt.Sprintf("-invented-dir%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, id+".jsonl")
		appendFile(t, path, claudeRecord("invented-claude", 1, 512))
		rows = append(rows, hcomidentity.Row{Name: fmt.Sprintf("impl-%d", i), Tool: "claude", Directory: fmt.Sprintf("/invented/dir%d", i), SessionID: id})
		paths = append(paths, path)
	}
	f.rows = rows
	o := newObserver(f, &fakeClock{now: time.Unix(1_700_000_000, 0)}, fsnotify.NewWatcher)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o.Run(ctx)
	watched := 0
	for _, row := range rows {
		got, _ := o.lookupState(KeyFor(row))
		if got.phase != PhaseTailing {
			t.Fatalf("%s phase %s err %q", row.Name, got.phase, got.err)
		}
		if got.watched {
			watched++
		}
	}
	if watched != MaxWatchDirectories {
		t.Fatalf("watched %d directories, ceiling %d", watched, MaxWatchDirectories)
	}
	last := rows[len(rows)-1]
	if got, _ := o.lookupState(KeyFor(last)); got.watched {
		t.Fatal("session above the ceiling reported as watched")
	}
	appendFile(t, paths[len(paths)-1], claudeRecord("invented-claude-swept", 99, 512))
	o.sweep()
	if got, _ := o.lookupState(KeyFor(last)); got.vitals.Model != "invented-claude-swept" {
		t.Fatalf("sweep did not advance the unwatched session: %+v", got)
	}
}

// Reddens: a key without agent_id (subagent answered with the parent's vitals).
func TestSubagentKeyDistinctFromParent(t *testing.T) {
	f := newFixture(t)
	parentID := "73300000-0000-4000-8000-000000000733"
	parentPath := filepath.Join(f.home, ".claude", "projects", "-invented-parent", parentID+".jsonl")
	agentID := "a35b593a6be7a9ba5"
	childPath := filepath.Join(strings.TrimSuffix(parentPath, ".jsonl"), "subagents", "agent-"+agentID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(childPath), 0o755); err != nil {
		t.Fatal(err)
	}
	appendFile(t, parentPath, claudeRecord("invented-parent", 10, 512))
	appendFile(t, childPath, `{"isSidechain":true,"type":"assistant","message":{"model":"invented-subagent","usage":{"input_tokens":13,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1}}}`+"\n")
	parent := hcomidentity.Row{Name: "impl-pare", Tool: "claude", Directory: "/invented/parent", SessionID: parentID}
	child := hcomidentity.Row{Name: "pare_general_purpose_1", Tool: "claude", SessionID: parentID, AgentID: agentID, ParentSessionID: parentID, ParentDirectory: "/invented/parent"}
	f.rows = []hcomidentity.Row{parent, child}
	o := newObserver(f, &fakeClock{now: time.Unix(1_700_000_000, 0)}, nil)
	o.sync(f.rows)
	p, _ := o.Lookup(parent)
	c, ok := o.Lookup(child)
	if !ok || p.Vitals.Model != "invented-parent" || c.Vitals.Model != "invented-subagent" {
		t.Fatalf("parent %+v child %+v ok=%v", p.Vitals, c.Vitals, ok)
	}
}

// Miss rule lives in Lookup: no vitals yet is a miss, so a caller never
// inspects Phase. Reddens: awaiting_file answered as a hit with empty vitals.
func TestLookupMissesWithoutVitals(t *testing.T) {
	f := newFixture(t)
	o := newObserver(f, &fakeClock{now: time.Unix(1_700_000_000, 0)}, nil)
	o.sync(f.rows)
	if _, ok := o.Lookup(f.rows[0]); ok {
		t.Fatal("awaiting_file must be a miss")
	}
	if _, ok := o.Lookup(hcomidentity.Row{Tool: "claude", SessionID: "unknown"}); ok {
		t.Fatal("unknown session must be a miss")
	}
}

// Not a gate: quoted in the report. 60 sessions each appending one turn per
// 30 s, compressed into one sweep per iteration.
func BenchmarkSixtySessionsSweep(b *testing.B) {
	home := b.TempDir()
	rows := make([]hcomidentity.Row, 0, 60)
	paths := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("73100000-0000-4000-8000-%012d", i)
		dir := filepath.Join(home, ".claude", "projects", fmt.Sprintf("-bench%d", i))
		_ = os.MkdirAll(dir, 0o755)
		path := filepath.Join(dir, id+".jsonl")
		_ = os.WriteFile(path, []byte(claudeRecord("bench", 1, 64<<10)), 0o644)
		rows = append(rows, hcomidentity.Row{Name: fmt.Sprintf("b%d", i), Tool: "claude", Directory: fmt.Sprintf("/bench%d", i), SessionID: id})
		paths = append(paths, path)
	}
	o := New(Options{Roster: func() ([]hcomidentity.Row, error) { return rows, nil }, Home: home, Poll: time.Hour, Sweep: time.Hour})
	o.sync(rows)
	chunk := claudeRecord("bench", 2, 64<<10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, path := range paths {
			f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
			_, _ = f.WriteString(chunk)
			_ = f.Close()
		}
		o.sweep()
	}
}

// Reddens: a roster blip forcing an awaiting_file session into tailing with
// no path (advance would open "" and record an error).
func TestRosterBlipRestoresPhaseFromPath(t *testing.T) {
	f := newFixture(t)
	appendFile(t, f.codex, codexRecords("invented-codex", 5, 1024))
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	o := newObserver(f, clock, nil)
	o.sync(f.rows) // claude: awaiting_file (no file); codex: tailing
	o.sync(nil)    // both ended
	o.sync(f.rows) // both back
	claude, _ := o.lookupState(KeyFor(f.rows[0]))
	codex, _ := o.lookupState(KeyFor(f.rows[1]))
	if claude.phase != PhaseAwaitingFile || claude.err != "" || codex.phase != PhaseTailing {
		t.Fatalf("claude %s err %q, codex %s", claude.phase, claude.err, codex.phase)
	}
}
