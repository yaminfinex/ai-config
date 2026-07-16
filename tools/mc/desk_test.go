package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Fixtures: two missions owned by human-yamen. mission-one carries the
// zone coverage (loops, blocked/unblocked decide/reply, FYI, shapeless,
// made-for-you ruling, withdrawn trace) and no milestones; mission-two
// carries the milestone narrative.
func deskFixtureStatuses() []missionStatus {
	return []missionStatus{
		{
			OK: true, Slug: "mission-one", MissionDir: "/missions/mission-one",
			Manifest: missionManifest{Mission: "mission-one", Authority: "vile", Owner: "human-yamen", Status: "active"},
			Board: missionBoard{Available: true, Total: 1, Tasks: []missionTask{
				{ID: "TASK-1", Title: "Cockpit rebuild queue", Status: "To Do"},
			}},
			Asks: missionAsks{Available: true, Total: 7, Entities: []askEntity{
				{
					// Listed before its group's open items on purpose: the trace
					// must still attach to the group it cleared from.
					ID: "withdrawn", Kind: "ask", State: "closed", Outcome: "superseded", Asker: "builder-gini", AddressedTo: "human-yamen",
					CreatedAt: "2026-07-16T03:00:00Z", UpdatedAt: "2026-07-16T03:30:00Z", Expects: "decide",
					Anchor:  askReference{Type: "task", Ref: "TASK-1"},
					Framing: askFraming{Context: "ctx", Question: "old duplicate question"},
					Traces:  []askTrace{{Action: "withdraw", Actor: "vile", At: "2026-07-16T03:30:00Z", Citation: &askReference{Type: "entity", Ref: "z2-blocked"}}},
				},
				{
					ID: "loop-answered", Kind: "ask", State: "open", Asker: "human-yamen", AddressedTo: "builder-koze",
					CreatedAt: "2026-07-16T09:00:00Z", UpdatedAt: "2026-07-16T09:30:00Z", Expects: "reply",
					Anchor:  askReference{Type: "task", Ref: "TASK-1"},
					Framing: askFraming{Context: "ctx", Question: "why did TASK-226 appear?"},
					Replies: []askReply{{ID: "r1", Actor: "builder-koze", At: "2026-07-16T09:20:00Z", Prose: "promoted by mote's FYI; intent-gating now prevents it"}},
				},
				{
					ID: "loop-waiting", Kind: "ask", State: "open", Asker: "human-yamen", AddressedTo: "builder-hera",
					CreatedAt: "2026-07-16T08:00:00Z", UpdatedAt: "2026-07-16T08:00:00Z", Expects: "reply",
					Anchor:  askReference{Type: "mission", Ref: "mission-one"},
					Framing: askFraming{Context: "ctx", Question: "confirm TASK-234 numbers read"},
				},
				{
					ID: "z2-blocked", Kind: "ask", State: "open", Asker: "builder-gini", AddressedTo: "human-yamen",
					CreatedAt: "2026-07-16T10:00:00Z", UpdatedAt: "2026-07-16T10:00:00Z", Expects: "decide",
					Blocking: &askBlocking{Fact: "task-31 worktree idle", Actor: "builder-gini", At: "2026-07-16T10:00:00Z"},
					Anchor:   askReference{Type: "task", Ref: "TASK-1"},
					Framing:  askFraming{Context: "ctx", Question: "A/B reviewer for grok pilot"},
				},
				{
					ID: "z2-open", Kind: "ask", State: "open", Asker: "builder-hera", AddressedTo: "human-yamen",
					CreatedAt: "2026-07-16T07:00:00Z", UpdatedAt: "2026-07-16T07:00:00Z", Expects: "reply",
					Anchor:  askReference{Type: "task", Ref: "TASK-1"},
					Framing: askFraming{Context: "ctx", Question: "confirm rollout order"},
				},
				{
					ID: "fyi-read", Kind: "ask", State: "open", Asker: "builder-mote", AddressedTo: "human-yamen",
					CreatedAt: "2026-07-16T06:00:00Z", UpdatedAt: "2026-07-16T06:00:00Z", Expects: "read",
					Anchor:  askReference{Type: "task", Ref: "TASK-1"},
					Framing: askFraming{Context: "ctx", Question: "run-log addendum posted"},
				},
				{
					ID: "shapeless", Kind: "ask", State: "open", Asker: "builder-nigo", AddressedTo: "human-yamen",
					CreatedAt: "2026-07-16T05:00:00Z", UpdatedAt: "2026-07-16T05:00:00Z", Expects: "",
					Framing: askFraming{Question: "have a look when free"},
				},
				{
					ID: "made-for-you", Kind: "ruling", State: "open", Asker: "vile", AddressedTo: "human-yamen",
					CreatedAt: "2026-07-16T04:00:00Z", UpdatedAt: "2026-07-16T04:00:00Z",
					Anchor:  askReference{Type: "task", Ref: "TASK-1"},
					Framing: askFraming{Question: "chose reviewer swap gini to raro"},
					Rulings: []askRuling{{Prose: "swapped for load", Actor: "vile", At: "2026-07-16T04:00:00Z"}},
				},
			}},
		},
		{
			OK: true, Slug: "mission-two", MissionDir: "/missions/mission-two",
			Manifest: missionManifest{Mission: "mission-two", Authority: "vile", Owner: "human-yamen", Status: "active"},
			Board: missionBoard{Available: true, Total: 2, Tasks: []missionTask{
				{ID: "TASK-1", Title: "First job", Status: "Done"},
				{ID: "TASK-2", Title: "Second job", Status: "In Progress"},
			}},
			Asks: missionAsks{Available: true},
		},
	}
}

const deskFixtureMilestones = `Active milestones (2):
  m-1: phase two: build (0/1 done)
  m-2: phase three: ship (0/2 done)

Completed milestones (1):
  m-0: phase one: capture (1/1 done)
`

func deskWeb(t *testing.T, statuses []missionStatus) *Web {
	t.Helper()
	dir := t.TempDir()
	all, err := json.Marshal(statuses)
	if err != nil {
		t.Fatal(err)
	}
	one, err := json.Marshal(statuses[0])
	if err != nil {
		t.Fatal(err)
	}
	writeFixture := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	allPath := writeFixture("all.json", string(all))
	onePath := writeFixture("one.json", string(one))
	milestonesPath := writeFixture("milestones.txt", deskFixtureMilestones)
	replyPath := writeFixture("reply.json", `{"ok":true,"verb":"asks.reply","slug":"mission-one","entity":{"id":"loop-answered","state":"open"}}`)
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
if [ "$1" = "status" ] && [ "$2" = "--all" ]; then cat %q; exit 0; fi
if [ "$1" = "status" ]; then cat %q; exit 0; fi
if [ "$1" = "backlog" ] && [ "$4" = "milestone" ]; then
  if [ "$3" = "mission-two" ]; then cat %q; else printf 'Active milestones (0):\n  (none)\n'; fi
  exit 0
fi
if [ "$1" = "asks" ] && [ "$4" = "reply" ]; then cat >/dev/null; cat %q; exit 0; fi
exit 2
`, allPath, onePath, milestonesPath, replyPath))
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	w := NewWeb(s, &Bus{}, nil, "human-yamen", "owner", "", newMissionResolver(mish, ""))
	w.now = func() time.Time { return time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC) }
	return w
}

func getDesk(t *testing.T, w *Web) string {
	t.Helper()
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("GET / = %d: %s", rw.Code, rw.Body.String())
	}
	return rw.Body.String()
}

func TestDeskZonesStrictOrderAndMembership(t *testing.T) {
	w := deskWeb(t, deskFixtureStatuses())
	// Transitional wire transport: a decide/reply raise still travelling as
	// a managed thread joins zone 2; an FYI-tier read raise does not.
	if err := w.store.Open("wire-raise", "hcom schema bump window", "ctx", "reply", "moment", "mission-two", "builder-muzo", nil, "owner", "managed"); err != nil {
		t.Fatal(err)
	}
	if err := w.store.Open("wire-inform", "tooling observation filed", "ctx", "read", "moment", "mission-two", "builder-mote", nil, "owner", "managed"); err != nil {
		t.Fatal(err)
	}
	body := getDesk(t, w)

	z1 := strings.Index(body, `id="z1"`)
	z2 := strings.Index(body, `id="z2"`)
	z3 := strings.Index(body, `id="z3"`)
	deskStart := strings.Index(body, `<section data-live="desk">`)
	if z1 < 0 || z2 < 0 || z3 < 0 || !(z1 < z2 && z2 < z3) {
		t.Fatalf("zone order broken: z1=%d z2=%d z3=%d", z1, z2, z3)
	}
	if between := body[deskStart:z1]; strings.Contains(between, "card") || strings.Contains(between, "strip") {
		t.Fatalf("content rendered above zone 1: %q", between)
	}

	// Zone 1: answered loop folds the answer in place with a JS-off composer.
	if !strings.Contains(body, "(1 answered, 1 waiting)") {
		t.Fatalf("zone-1 counts missing: %s", body[z1:z2])
	}
	for _, want := range []string{
		"why did TASK-226 appear?",
		"promoted by mote&#39;s FYI; intent-gating now prevents it",
		`action="/ask/loop-answered/reply?return=%2F%23z1"`,
		`<textarea name="prose"`,
		"waiting on builder-hera",
		"owner-outbound asks · board · mish status",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("zone 1 missing %q", want)
		}
	}

	// Zone 2: ALL expects decide/reply (entity + wire raise), blocked-first,
	// anchor-grouped with context once; FYI and made-for-you excluded;
	// shapeless renders degraded at the tail, uncounted.
	zone2 := body[z2:z3]
	if !strings.Contains(zone2, "(3 open · 1 blocked)") {
		t.Fatalf("zone-2 header counts wrong:\n%s", zone2)
	}
	blocked := strings.Index(zone2, "A/B reviewer for grok pilot")
	open := strings.Index(zone2, "confirm rollout order")
	raise := strings.Index(zone2, "hcom schema bump window")
	if blocked < 0 || open < 0 || raise < 0 {
		t.Fatalf("zone-2 items missing: blocked=%d open=%d raise=%d", blocked, open, raise)
	}
	if !(blocked < open) {
		t.Error("blocked item did not sort first within its group")
	}
	if !(open < raise) {
		t.Error("group carrying the blocked item did not sort first")
	}
	if !strings.Contains(zone2, "claims blocked: task-31 worktree idle") {
		t.Error("blocking claim badge missing")
	}
	if !strings.Contains(zone2, "ctx: Cockpit rebuild queue") {
		t.Error("anchor group context (board task title) missing")
	}
	if !strings.Contains(zone2, "withdraw by vile") || !strings.Contains(zone2, "cites: entity:z2-blocked") {
		t.Error("co-custodian withdrawal trace (with citation) missing from its group")
	}
	degraded := strings.Index(zone2, "degraded: targeted, no declared expects — builder-nigo")
	if degraded < 0 {
		t.Fatal("shapeless mention did not render as degraded tail item")
	}
	if degraded < blocked || degraded < raise {
		t.Error("degraded item rendered above counted items")
	}
	if strings.Contains(zone2, "run-log addendum posted") {
		t.Error("FYI-tier (read) entity leaked into zone 2")
	}
	if strings.Contains(zone2, "tooling observation filed") {
		t.Error("FYI-tier wire raise leaked into zone 2")
	}
	if strings.Contains(zone2, "chose reviewer swap") {
		t.Error("made-for-you ruling queued in zone 2")
	}

	// Zone 3: strips — narrative for mission-two, degraded-honest for
	// mission-one, made-for-you annotation, provenance line.
	zone3 := body[z3:]
	for _, want := range []string{
		"no narrative layer — board is task-grain only ⚠",
		"phase: phase two: build (1 of 3 milestones done)",
		"phase one: capture",
		"phase three: ship",
		"Second job — In Progress",
		"⚑ made for you (1): chose reviewer swap gini to raro",
		"/mission/mission-one/review",
		"crew-authored (board milestones) · board · mish status",
	} {
		if !strings.Contains(zone3, want) {
			t.Errorf("zone 3 missing %q", want)
		}
	}

	// Health signals live in the SYSTEM panel only (never interleaved):
	// the shapeless ask is unanchored, so the count renders there.
	panel := strings.Index(body, `data-live="object-panel"`)
	unanchored := strings.Index(body, "unanchored asks: 1")
	if unanchored < 0 {
		t.Fatal("unanchored pile missing from SYSTEM panel")
	}
	if unanchored < panel {
		t.Error("health signal rendered outside the SYSTEM panel")
	}
	if strings.Contains(body[deskStart:z3], "unanchored asks") {
		t.Error("health signal interleaved with desk zones")
	}
}

func TestDeskZone1ComposerReplyReturnsToDesk(t *testing.T) {
	w := deskWeb(t, deskFixtureStatuses())
	form := url.Values{"if_updated_at": {"2026-07-16T09:30:00Z"}, "prose": {"thanks, close it out"}}
	req := httptest.NewRequest(http.MethodPost, "/ask/loop-answered/reply?return=%2F%23z1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, req)
	if rw.Code != http.StatusSeeOther || rw.Header().Get("Location") != "/#z1" {
		t.Fatalf("zone-1 reply = %d Location %q, want redirect back to the desk", rw.Code, rw.Header().Get("Location"))
	}
}

// The proof test for acceptance (4): every crew-authored string a strip
// renders is present verbatim in the board content mish handed us — mc
// invents no rollup text.
func TestStripTextIsSubsetOfBoardContent(t *testing.T) {
	w := deskWeb(t, deskFixtureStatuses())
	model := w.buildDesk()
	var corpus []string
	for _, m := range parseMilestones(deskFixtureMilestones) {
		corpus = append(corpus, m.Title)
	}
	for _, st := range deskFixtureStatuses() {
		for _, task := range st.Board.Tasks {
			corpus = append(corpus, task.Title, task.Status)
		}
		for _, e := range st.Asks.Entities {
			corpus = append(corpus, e.Framing.Question)
		}
	}
	if len(model.Strips) != 2 {
		t.Fatalf("strips = %d, want 2", len(model.Strips))
	}
	for _, strip := range model.Strips {
		for _, text := range strip.NarrativeText() {
			found := false
			for _, source := range corpus {
				if text == source {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("strip %s renders %q which is not board content", strip.Slug, text)
			}
		}
	}
}

// A7 regression: a mission with unreviewed made-for-you decisions cannot
// rank below one with none when every other key is equal — even when the
// debt-free mission moved more recently.
func TestStripOrderReviewDebtCannotSink(t *testing.T) {
	older := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	strips := []missionStrip{
		{Slug: "moved-recently", NeedsYou: 1, Blocked: 1, ReviewDebt: 0, lastMove: newer},
		{Slug: "owes-review", NeedsYou: 1, Blocked: 1, ReviewDebt: 1, lastMove: older},
	}
	sortStrips(strips)
	if strips[0].Slug != "owes-review" {
		t.Fatalf("strip order = %s, %s — review debt sank below an otherwise-equal mission", strips[0].Slug, strips[1].Slug)
	}
	// And the tiebreak stays most-moved when debt is equal too.
	strips = []missionStrip{
		{Slug: "quiet", lastMove: older},
		{Slug: "moved", lastMove: newer},
	}
	sortStrips(strips)
	if strips[0].Slug != "moved" {
		t.Fatalf("most-moved tiebreak broken: %s first", strips[0].Slug)
	}
}

func TestSpinningRendersEvidenceAndResetsOnBoardMove(t *testing.T) {
	statuses := deskFixtureStatuses()
	w := deskWeb(t, statuses)
	if err := w.store.Open("chatter", "busy thread", "ctx", "read", "moment", "mission-two", "builder-loke", nil, "", "observed"); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	for i := 0; i < spinEventFloor+10; i++ {
		msgAt := t0.Add(5 * time.Minute)
		if err := w.store.Link("chatter", Msg{BusID: int64(100 + i), From: "builder-loke", Text: "spin", TS: msgAt.Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
	}

	w.now = func() time.Time { return t0 }
	first := getDesk(t, w)
	if strings.Contains(first, "⚠ spinning") {
		t.Fatal("spinning rendered before the window floor passed")
	}

	w.now = func() time.Time { return t0.Add(spinWindowFloor + 10*time.Minute) }
	second := getDesk(t, w)
	want := fmt.Sprintf("⚠ spinning: %d bus events, 0 board moves since", spinEventFloor+10)
	if !strings.Contains(second, want) {
		t.Fatalf("spinning evidence missing %q:\n%s", want, second)
	}
	if !strings.Contains(second, "2026-07-16 12:00:00 UTC") {
		t.Error("spinning evidence does not render its window start")
	}

	// A board transition restarts the window: no spinning immediately after.
	if _, _ = w.motion.observe("mission-two", "different-board-state", w.now()); true {
		third := getDesk(t, w)
		if strings.Contains(third, "⚠ spinning") {
			t.Fatal("spinning survived an observed board transition")
		}
	}
}

func TestBoardMotionObserveTracksTransitions(t *testing.T) {
	bm := newBoardMotion()
	t0 := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	windowStart, lastMove := bm.observe("m", "fp1", t0)
	if !windowStart.Equal(t0) || !lastMove.IsZero() {
		t.Fatalf("first observation = window %v lastMove %v", windowStart, lastMove)
	}
	windowStart, lastMove = bm.observe("m", "fp1", t0.Add(time.Hour))
	if !windowStart.Equal(t0) || !lastMove.IsZero() {
		t.Fatalf("unchanged board moved the window: %v %v", windowStart, lastMove)
	}
	moveAt := t0.Add(2 * time.Hour)
	windowStart, lastMove = bm.observe("m", "fp2", moveAt)
	if !windowStart.Equal(moveAt) || !lastMove.Equal(moveAt) {
		t.Fatalf("transition not recorded: window %v lastMove %v", windowStart, lastMove)
	}
}

func TestMilestoneParsing(t *testing.T) {
	milestones := parseMilestones(deskFixtureMilestones)
	if len(milestones) != 3 {
		t.Fatalf("parsed %d milestones, want 3", len(milestones))
	}
	if milestones[0].ID != "m-1" || milestones[0].Title != "phase two: build" || milestones[0].Done != 0 || milestones[0].Total != 1 || milestones[0].Completed {
		t.Fatalf("active milestone parsed wrong: %+v", milestones[0])
	}
	if milestones[2].ID != "m-0" || milestones[2].Title != "phase one: capture" || !milestones[2].Completed || milestones[2].Done != 1 {
		t.Fatalf("completed milestone parsed wrong: %+v", milestones[2])
	}
	if got := parseMilestones("Active milestones (0):\n  (none)\n\nCompleted milestones (0):\n  (none)\n"); got != nil {
		t.Fatalf("empty listing parsed as %+v", got)
	}
}

// The token sheet's standing review blocker, encoded: the skin consumes
// semantic tokens only — no raw hex, and the retired legacy vars stay dead.
func TestTemplateStyleUsesTokensOnly(t *testing.T) {
	start := strings.Index(pageTpl, "<style>")
	end := strings.Index(pageTpl, "</style>")
	if start < 0 || end < 0 {
		t.Fatal("style block not found")
	}
	style := pageTpl[start:end]
	if hex := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`).FindAllString(style, -1); len(hex) > 0 {
		t.Fatalf("raw hex in template style block: %v", hex)
	}
	for _, legacy := range []string{"var(--fg)", "var(--dim)", "var(--line)", "var(--accent)", "var(--bg)", "var(--card)", "var(--warn)", "--accent:", "--dim:", "--warn:"} {
		if strings.Contains(pageTpl, legacy) {
			t.Errorf("retired legacy var still referenced: %s", legacy)
		}
	}
	if !strings.Contains(style, "var(--c-needs-you)") || !strings.Contains(style, "var(--t-body)") || !strings.Contains(style, "var(--s-") {
		t.Error("style block does not consume the semantic token sheet")
	}
}

func TestGraphSVGCarriesNoRawHex(t *testing.T) {
	model := &graphModel{AsOf: time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)}
	svg := string(renderGraphSVG(model, "", "", "human-yamen", url.Values{}))
	if hex := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`).FindAllString(svg, -1); len(hex) > 0 {
		t.Fatalf("raw hex in graph SVG: %v", hex)
	}
}
