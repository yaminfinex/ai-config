package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fold() is pure store-fold logic — no bus calls — so it is tested with
// synthetic events against a temp journal.

func testIngestor(t *testing.T) (*Ingestor, *Store) {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return &Ingestor{store: s, user: "human-yamen", seat: "owner"}, s
}

func busEvent(id int64, from, thread, text, intent string, mentions ...string) BusEvent {
	ev := BusEvent{ID: id, TS: "2026-07-15T10:00:00Z", Type: "message"}
	ev.Data.From = from
	ev.Data.Thread = thread
	ev.Data.Text = text
	ev.Data.Intent = intent
	ev.Data.Mentions = mentions
	return ev
}

func fold(t *testing.T, in *Ingestor, ev BusEvent) {
	t.Helper()
	if err := in.fold(ev); err != nil {
		t.Fatalf("fold #%d: %v", ev.ID, err)
	}
}

// hcom stamps an implicit @bigboss on every mention-free send; that must
// never open a managed thread on the desk.
func TestImplicitBigbossOpensObserved(t *testing.T) {
	in, s := testIngestor(t)
	fold(t, in, busEvent(1, "builder-gemi", "task-42-chatter", "starting on the parser.", "inform", "bigboss"))

	th := s.Get("task-42-chatter")
	if th == nil {
		t.Fatal("thread not tracked at all")
	}
	if th.Grade != "observed" {
		t.Fatalf("grade = %q, want observed", th.Grade)
	}
	if th.Turn == "owner" {
		t.Fatal("observed thread must never be owner's turn")
	}
	if got := s.List("open", "managed"); len(got) != 0 {
		t.Fatalf("managed list has %d threads, want 0", len(got))
	}
	if got := s.List("", "observed"); len(got) != 1 {
		t.Fatalf("observed list has %d threads, want 1", len(got))
	}
}

func TestExplicitSeatRaiseOpensManaged(t *testing.T) {
	in, s := testIngestor(t)
	fold(t, in, busEvent(2, "vile", "task-1-review", "@owner need a decision on the journal format.", "request", "owner", "bigboss"))

	th := s.Get("task-1-review")
	if th == nil {
		t.Fatal("raise did not open a thread")
	}
	if th.Grade != "managed" {
		t.Fatalf("grade = %q, want managed", th.Grade)
	}
	if th.Expects != "reply" {
		t.Fatalf("expects = %q, want reply (intent=request)", th.Expects)
	}
	if th.Turn != "owner" {
		t.Fatalf("turn = %q, want owner", th.Turn)
	}
}

func TestThreadlessRaiseOpensDeskThread(t *testing.T) {
	in, s := testIngestor(t)
	fold(t, in, busEvent(7, "vile", "", "ping @owner directly.", "inform", "owner"))

	th := s.Get("desk-7")
	if th == nil {
		t.Fatal("threadless raise did not open desk thread")
	}
	if th.Grade != "managed" {
		t.Fatalf("grade = %q, want managed", th.Grade)
	}
}

func TestThreadlessChatterIgnored(t *testing.T) {
	in, s := testIngestor(t)
	fold(t, in, busEvent(3, "worker-mate", "", "ok on it.", "ack", "bigboss"))

	if got := s.List("", ""); len(got) != 0 {
		t.Fatalf("tracked %d threads for threadless chatter, want 0", len(got))
	}
}

func TestExplicitRaisePromotesObservedPreservingHistory(t *testing.T) {
	in, s := testIngestor(t)
	fold(t, in, busEvent(10, "builder-gemi", "task-9-work", "step one done.", "inform", "bigboss"))
	fold(t, in, busEvent(11, "worker-vele", "task-9-work", "picking up step two.", "inform", "bigboss"))
	fold(t, in, busEvent(12, "builder-gemi", "task-9-work", "blocked — need @owner to decide.", "request", "owner"))

	th := s.Get("task-9-work")
	if th.Grade != "managed" {
		t.Fatalf("grade = %q, want managed after explicit raise", th.Grade)
	}
	if len(th.Msgs) != 3 {
		t.Fatalf("history = %d msgs, want 3 (promotion must preserve observed history)", len(th.Msgs))
	}
	if th.Expects != "reply" {
		t.Fatalf("expects = %q, want reply", th.Expects)
	}
	if th.Turn != "owner" {
		t.Fatalf("turn = %q, want owner", th.Turn)
	}
	// Participants accreted while observed survive promotion.
	if !contains(th.With, "worker-vele") {
		t.Fatalf("with = %v, want worker-vele preserved", th.With)
	}
}

// Event #87580 had this exact defect shape: a mention-free worker ack was
// stamped mentions:[owner] by hcom. The wire field alone must not promote the
// observed thread or make it the owner's turn.
func TestImplicitOwnerMentionDoesNotPromoteObserved(t *testing.T) {
	in, s := testIngestor(t)
	fold(t, in, busEvent(87579, "reviewer-gini", "task-26-membership-grouping", "reviewing the grouping.", "inform", "bigboss"))
	fold(t, in, busEvent(87580, "builder-luga", "task-26-membership-grouping", "ACK: I will implement the requested fix.", "ack", "owner"))

	th := s.Get("task-26-membership-grouping")
	if th.Grade != "observed" || th.Turn == "owner" {
		t.Fatalf("grade=%q turn=%q, want observed and not owner's turn", th.Grade, th.Turn)
	}
	if len(th.Msgs) != 2 {
		t.Fatalf("msgs = %d, want both observed messages preserved", len(th.Msgs))
	}
}

func TestRaiseVerbExplicitTargetPromotes(t *testing.T) {
	in, s := testIngestor(t)
	fold(t, in, busEvent(90, "builder-gemi", "task-raise", "working normally.", "inform", "bigboss"))
	fold(t, in, busEvent(91, "builder-gemi", "task-raise", "@owner\nCONTEXT: the build gate needs a ruling\nEXPECTS: decide", "request", "owner"))

	th := s.Get("task-raise")
	if th.Grade != "managed" || th.Turn != "owner" {
		t.Fatalf("grade=%q turn=%q, want managed/owner", th.Grade, th.Turn)
	}
}

func TestExplicitSeatMentionAliases(t *testing.T) {
	tests := []struct {
		text string
		seat string
		want bool
	}{
		{"please decide @human", "human", true},
		{"please decide @owner", "human", true},
		{"please decide @bigboss", "human", true},
		{"mention supplied only on the wire", "human", false},
		{"wrong case @OWNER", "human", false},
		{"different target @owner-helper", "human", false},
		{"remote target @owner:BOXE", "human", false},
	}
	for _, tt := range tests {
		if got := explicitSeatMention(tt.text, tt.seat); got != tt.want {
			t.Errorf("explicitSeatMention(%q, %q) = %v, want %v", tt.text, tt.seat, got, tt.want)
		}
	}
}

func TestObservedStaysObservedAcrossTraffic(t *testing.T) {
	in, s := testIngestor(t)
	fold(t, in, busEvent(20, "builder-gemi", "task-8-noise", "a", "inform", "bigboss"))
	fold(t, in, busEvent(21, "worker-mate", "task-8-noise", "b", "inform", "bigboss"))
	fold(t, in, busEvent(22, "builder-gemi", "task-8-noise", "c", "ack", "bigboss"))

	th := s.Get("task-8-noise")
	if th.Grade != "observed" {
		t.Fatalf("grade = %q, want observed", th.Grade)
	}
	if len(th.Msgs) != 3 {
		t.Fatalf("msgs = %d, want 3", len(th.Msgs))
	}
	if th.Turn == "owner" {
		t.Fatal("observed thread drifted into owner's turn")
	}
}

func TestManagedThreadLinksMentionFreeFollowups(t *testing.T) {
	in, s := testIngestor(t)
	fold(t, in, busEvent(30, "vile", "task-5-run", "raised at @owner.", "request", "owner"))
	fold(t, in, busEvent(31, "vile", "task-5-run", "follow-up detail, no mentions.", "inform", "bigboss"))

	th := s.Get("task-5-run")
	if th.Grade != "managed" || len(th.Msgs) != 2 {
		t.Fatalf("grade = %q msgs = %d, want managed with 2", th.Grade, len(th.Msgs))
	}
	if th.Turn != "owner" {
		t.Fatalf("turn = %q, want owner", th.Turn)
	}
}

func TestHumanReplyFlipsTurn(t *testing.T) {
	in, s := testIngestor(t)
	fold(t, in, busEvent(40, "vile", "task-6-turn", "your call, @owner.", "request", "owner"))
	fold(t, in, busEvent(41, "human-yamen", "task-6-turn", "approved.", "inform"))

	if th := s.Get("task-6-turn"); th.Turn == "owner" {
		t.Fatalf("turn = %q, want flipped off owner after human reply", th.Turn)
	}
}

// ~/.mc/journal.jsonl is live production state: entries written before the
// grade field existed must replay as managed.
func TestPreGradeJournalReplaysAsManaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	preGrade := `{"ts":"2026-07-14T09:00:00Z","op":"open","thread":"t-old","title":"old thread","context":"ctx","expects":"reply","weight":"moment","by":"human-yamen","with":["vile"],"turn":"vile"}
{"ts":"2026-07-14T09:01:00Z","op":"link","thread":"t-old","bus_id":100,"from":"vile","text":"hello","msg_ts":"2026-07-14T09:01:00Z"}
{"ts":"2026-07-14T09:02:00Z","op":"turn","thread":"t-old","turn":"owner"}
{"ts":"2026-07-14T09:03:00Z","op":"cursor","bus_id":100}
`
	if err := os.WriteFile(path, []byte(preGrade), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	th := s.Get("t-old")
	if th == nil {
		t.Fatal("pre-grade thread lost on replay")
	}
	if th.Grade != "managed" {
		t.Fatalf("grade = %q, want managed default for pre-grade entries", th.Grade)
	}
	if len(th.Msgs) != 1 || th.Turn != "owner" || s.Cursor() != 100 {
		t.Fatalf("replay drift: msgs=%d turn=%q cursor=%d", len(th.Msgs), th.Turn, s.Cursor())
	}
	if got := s.List("open", "managed"); len(got) != 1 {
		t.Fatalf("managed list has %d, want 1", len(got))
	}
}

// Grade round-trips through the journal: an observed open + promote replays
// back to the same state.
func TestGradeSurvivesReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	in := &Ingestor{store: s, user: "human-yamen", seat: "owner"}
	fold(t, in, busEvent(50, "builder-gemi", "task-r-one", "obs.", "inform", "bigboss"))
	fold(t, in, busEvent(51, "builder-gemi", "task-r-two", "obs then raised.", "inform", "bigboss"))
	fold(t, in, busEvent(52, "builder-gemi", "task-r-two", "now raised @owner.", "request", "owner"))

	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if g := s2.Get("task-r-one").Grade; g != "observed" {
		t.Fatalf("task-r-one replayed as %q, want observed", g)
	}
	two := s2.Get("task-r-two")
	if two.Grade != "managed" || len(two.Msgs) != 2 {
		t.Fatalf("task-r-two replayed grade=%q msgs=%d, want managed/2", two.Grade, len(two.Msgs))
	}
}

func TestObservedLifecycleWritesAreRefused(t *testing.T) {
	_, s := testIngestor(t)
	if err := s.Open("task-read-only", "observed", "ctx", "read", "moment", "", "worker", nil, "worker", "observed"); err != nil {
		t.Fatal(err)
	}
	w := NewWeb(s, &Bus{}, nil, "human-yamen", "owner", "", nil)
	get := httptest.NewRecorder()
	w.Routes().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/thread/task-read-only", nil))
	for _, action := range []string{"/reply", "/close", "/reopen", "/retitle"} {
		if strings.Contains(get.Body.String(), action) {
			t.Errorf("observed thread page exposes lifecycle action %q", action)
		}
	}

	tests := []struct {
		path string
		form url.Values
	}{
		{"/thread/task-read-only/reply", url.Values{"text": {"bypass reply"}}},
		{"/thread/task-read-only/close", url.Values{"resolution": {"bypass close"}}},
		{"/thread/task-read-only/reopen", nil},
		{"/thread/task-read-only/retitle", url.Values{"title": {"bypass retitle"}}},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, req)
		if rw.Code != http.StatusSeeOther || !strings.Contains(rw.Header().Get("Location"), "observed+threads+are+read-only") {
			t.Errorf("POST %s = %d Location %q, want refused redirect", tt.path, rw.Code, rw.Header().Get("Location"))
		}
	}

	th := s.Get("task-read-only")
	if th.Status != "open" || th.Grade != "observed" || th.Resolution != "" {
		t.Fatalf("refused writes changed thread: status=%q grade=%q resolution=%q", th.Status, th.Grade, th.Resolution)
	}
	if err := s.Close(th.ID, "direct bypass"); err == nil {
		t.Fatal("store accepted a direct close of an observed thread")
	}
}

func TestExplicitRaiseReopensSyntheticClosedObservedThread(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	journal := `{"ts":"2026-07-15T09:00:00Z","op":"open","thread":"task-corrupt","title":"observed","grade":"observed","expects":"read","by":"worker","turn":"worker"}
{"ts":"2026-07-15T09:01:00Z","op":"close","thread":"task-corrupt","resolution":"stale synthetic close"}
`
	if err := os.WriteFile(path, []byte(journal), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	in := &Ingestor{store: s, user: "human-yamen", seat: "owner"}
	fold(t, in, busEvent(60, "builder-gemi", "task-corrupt", "explicit raise @owner", "request", "owner"))

	th := s.Get("task-corrupt")
	if th.Grade != "managed" || th.Status != "open" || th.Resolution != "" || th.Turn != "owner" {
		t.Fatalf("promoted thread grade=%q status=%q resolution=%q turn=%q, want managed/open/empty/owner", th.Grade, th.Status, th.Resolution, th.Turn)
	}

	// Recovery is journaled, not merely repaired in memory.
	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	replayed := s2.Get("task-corrupt")
	if replayed.Grade != "managed" || replayed.Status != "open" || replayed.Resolution != "" {
		t.Fatalf("replayed promotion grade=%q status=%q resolution=%q", replayed.Grade, replayed.Status, replayed.Resolution)
	}
}
