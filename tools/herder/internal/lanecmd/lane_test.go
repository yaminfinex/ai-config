package lanecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/hcomidentity"
)

type harness struct {
	t      *testing.T
	state  string
	roster []hcomidentity.Row
	clock  time.Time
}

func newHarness(t *testing.T, live ...string) *harness {
	t.Helper()
	t.Setenv("HCOM_PROCESS_ID", "")
	h := &harness{t: t, state: t.TempDir(), clock: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)}
	t.Setenv("HERDER_STATE_DIR", h.state)
	for _, name := range live {
		h.roster = append(h.roster, hcomidentity.Row{Name: name})
	}
	return h
}

func (h *harness) run(args ...string) (int, string, string) {
	h.t.Helper()
	h.clock = h.clock.Add(time.Second)
	var stdout, stderr bytes.Buffer
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) { return h.roster, nil }, now: func() time.Time { return h.clock }}
	code := run(append(args, "--by", "owner", "--by-kind", "user"), &stdout, &stderr, deps)
	return code, stdout.String(), stderr.String()
}

func (h *harness) ok(args ...string) string {
	h.t.Helper()
	code, stdout, stderr := h.run(args...)
	if code != 0 {
		h.t.Fatalf("%v: code=%d stderr=%q", args, code, stderr)
	}
	return stdout
}

func (h *harness) refused(want int, needle string, args ...string) {
	h.t.Helper()
	code, _, stderr := h.run(args...)
	if code != want || !strings.Contains(stderr, needle) {
		h.t.Fatalf("%v: code=%d stderr=%q, want %d containing %q", args, code, stderr, want, needle)
	}
}

func (h *harness) proj() *agentstore.Projection {
	h.t.Helper()
	proj, err := agentstore.Open(h.state, nil).Replay()
	if err != nil {
		h.t.Fatal(err)
	}
	return proj
}

func (h *harness) show(lane string) laneOutput {
	h.t.Helper()
	var out laneOutput
	if err := json.Unmarshal([]byte(h.ok("show", lane, "--json")), &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func TestNewCreatesTheLaneAndMovesTheLeadIn(t *testing.T) {
	h := newHarness(t, "lead-a", "seat-b")
	stdout := h.ok("new", "Refit", "--lead", "lead-a")
	id := strings.TrimSpace(strings.TrimPrefix(stdout, "lane="))
	lane := h.show("refit") // names resolve case-insensitively
	if lane.ID != id || lane.Name != "Refit" || lane.Lead != "lead-a" || !reflect.DeepEqual(lane.Members, []string{"lead-a"}) || lane.By != "owner" {
		t.Fatalf("lane = %+v", lane)
	}
	if h.show(id).ID != id {
		t.Fatal("lane did not resolve by id")
	}
	h.refused(2, "taken", "new", "REFIT", "--lead", "seat-b")
	h.refused(2, "--lead", "new", "other")
	h.refused(2, "usage", "new")
}

func TestRenameKeepsEarlierNames(t *testing.T) {
	h := newHarness(t, "lead-a")
	h.ok("new", "signup", "--lead", "lead-a")
	h.ok("rename", "signup", "turnkey-signup")
	h.ok("rename", "turnkey-signup", "turnkey")
	lane := h.show("turnkey")
	if lane.Name != "turnkey" || !reflect.DeepEqual(lane.Names, []string{"signup", "turnkey-signup"}) {
		t.Fatalf("lane = %+v", lane)
	}
	h.refused(1, "no lane", "show", "signup")
	text := h.ok("show", "turnkey")
	if !strings.Contains(text, "signup, turnkey-signup") {
		t.Fatalf("show text missing earlier names:\n%s", text)
	}
}

func TestMoveRefusesToStrandALaneWithoutItsLead(t *testing.T) {
	h := newHarness(t, "lead-a", "lead-b", "seat-c")
	h.ok("new", "one", "--lead", "lead-a")
	h.ok("new", "two", "--lead", "lead-b")
	h.ok("move", "seat-c", "one")
	h.refused(2, "leads lane one", "move", "lead-a", "two")
	h.refused(2, "leads lane one", "lead", "two", "lead-a")
	h.refused(2, "leads lane one", "new", "three", "--lead", "lead-a")
	h.ok("move", "lead-a", "one") // staying in its own lane is fine
	// Hand the lead over, then the old lead may go.
	h.ok("lead", "one", "seat-c")
	h.ok("move", "lead-a", "two")
	one, two := h.show("one"), h.show("two")
	if one.Lead != "seat-c" || !reflect.DeepEqual(one.Members, []string{"seat-c"}) || !reflect.DeepEqual(two.Members, []string{"lead-a", "lead-b"}) {
		t.Fatalf("one=%+v two=%+v", one, two)
	}
}

func TestLeadMovesTheSeatInWhenNeeded(t *testing.T) {
	h := newHarness(t, "lead-a", "seat-b")
	h.ok("new", "one", "--lead", "lead-a")
	h.ok("lead", "one", "seat-b")
	lane := h.show("one")
	if lane.Lead != "seat-b" || !reflect.DeepEqual(lane.Members, []string{"lead-a", "seat-b"}) {
		t.Fatalf("lane = %+v", lane)
	}
	before := len(h.proj().Latest("seat-b").Events)
	h.ok("lead", "one", "seat-b") // already in: no second assign
	if after := len(h.proj().Latest("seat-b").Events); after != before {
		t.Fatalf("lead re-assigned a seat already in the lane: %d -> %d events", before, after)
	}
}

func TestLinkUnlinkAndRepo(t *testing.T) {
	h := newHarness(t, "lead-a")
	h.ok("new", "one", "--lead", "lead-a")
	h.ok("link", "one", "infinex/app#12")
	h.ok("link", "one", "infinex/app#13")
	h.ok("unlink", "one", "infinex/app#12")
	h.ok("repo", "one", "infinex/api")
	h.ok("repo", "one", "infinex/web")
	h.ok("repo", "one", "infinex/api", "--remove")
	lane := h.show("one")
	if !reflect.DeepEqual(lane.PRs, []string{"infinex/app#13"}) || !reflect.DeepEqual(lane.Repos, []string{"infinex/web"}) {
		t.Fatalf("lane = %+v", lane)
	}
	h.refused(2, "owner/repo#number", "link", "one", "https://github.com/infinex/app/pull/12")
	h.refused(2, "owner/repo", "repo", "one", "app")
	h.refused(1, "no lane", "link", "nope", "infinex/app#1")
}

func TestListHidesLanesWithoutALiveMember(t *testing.T) {
	h := newHarness(t, "lead-a", "lead-b")
	if out := h.ok("list"); out != "" {
		t.Fatalf("empty store listed %q", out)
	}
	h.ok("new", "beta", "--lead", "lead-b")
	h.ok("new", "alpha", "--lead", "lead-a")
	h.roster = h.roster[:1] // lead-b leaves the roster
	out := h.ok("list")
	if !strings.Contains(out, "alpha") || strings.Contains(out, "beta") {
		t.Fatalf("list:\n%s", out)
	}
	var all []laneOutput
	if err := json.Unmarshal([]byte(h.ok("list", "--all", "--json")), &all); err != nil || len(all) != 2 || all[0].Name != "alpha" || all[1].Name != "beta" || len(all[1].Members) != 0 {
		t.Fatalf("list --all: %+v err=%v", all, err)
	}
	// A hidden lane's name is free again; the old one stays reachable by id.
	h.roster = append(h.roster, hcomidentity.Row{Name: "seat-c"})
	h.ok("new", "beta", "--lead", "seat-c")
	if got := h.show("BETA"); got.Lead != "seat-c" {
		t.Fatalf("reused name resolved to %+v", got)
	}
}

func TestAmbiguousNamePrefersTheLiveLane(t *testing.T) {
	h := newHarness(t, "lead-a", "lead-b")
	h.ok("new", "same", "--lead", "lead-b")
	h.roster = h.roster[:1]
	newID := strings.TrimSpace(strings.TrimPrefix(h.ok("new", "same", "--lead", "lead-a"), "lane="))
	if got := h.show("same"); got.ID != newID {
		t.Fatalf("resolved %s, want the live lane %s", got.ID, newID)
	}
	h.roster = nil // nothing live: two matches, neither live
	h.refused(2, "ambiguous", "show", "same")
}

func TestUnavailableRosterStillWrites(t *testing.T) {
	h := newHarness(t)
	var stdout, stderr bytes.Buffer
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) { return nil, errors.New("hcom down") }, now: time.Now}
	if code := run([]string{"new", "one", "--lead", "lead-a"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stderr.String(), "hcom down") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if len(h.proj().Lanes) != 1 {
		t.Fatal("lane not written")
	}
}
