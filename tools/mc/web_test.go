package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestDerivedTitleUsesFirstSentenceAndCapsAt80Runes(t *testing.T) {
	if got := derivedTitle("Ship the simpler compose. More detail follows."); got != "Ship the simpler compose" {
		t.Fatalf("derived title = %q", got)
	}
	long := strings.Repeat("ø", 90)
	if got := derivedTitle(long); len([]rune(got)) != 80 {
		t.Fatalf("derived title has %d runes, want 80", len([]rune(got)))
	}
}

func TestGraphRendersContractSignalsAndURLState(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open("task-x", "Managed work", "ctx", "reply", "moment", "mission-one", "worker-kolu", nil, "owner", "managed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Open("task-observed", "Observed work", "", "read", "moment", "mission-one", "reviewer-meze", nil, "reviewer-meze", "observed"); err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(dir, "agent")
	if err := os.Mkdir(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(dir, "event-calls")
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
if [ "$1" = list ]; then
  printf '%%s\n' '[{"name":"reviewer-meze","status":"listening","directory":%q,"tool":"claude","unread_count":2},{"name":"builder-lobo","status":"active","directory":%q,"tool":"codex"},{"name":"worker-kolu","status":"blocked","directory":%q,"tool":"codex"},{"name":"worker-suna","status":"listening","directory":%q,"tool":"codex"}]'
  exit 0
fi
printf '%%s\n' "$*" >> %q
case " $* " in
  *" --mention "*) cat <<'EOF'
{"id":83270,"ts":"2026-07-15T05:40:00Z","type":"message","data":{"from":"reviewer-meze","text":"question"}}
{"id":83271,"ts":"2026-07-15T05:41:00Z","type":"message","data":{"from":"builder-lobo","text":"answer","reply_to_local":83270}}
{"id":83272,"ts":"2026-07-15T05:42:00Z","type":"message","data":{"from":"worker-suna","text":"plain","mentions":["bigboss"]}}
{"id":83273,"ts":"2026-07-15T05:43:00Z","type":"message","data":{"from":"worker-kolu","text":"raise","thread":"task-x","mentions":["owner"]}}
{"id":83274,"ts":"2026-07-15T05:44:00Z","type":"message","data":{"from":"human-yamen","text":"answer","thread":"task-x","mentions":["worker-kolu"]}}
{"id":83275,"ts":"2026-07-15T05:45:00Z","type":"message","data":{"from":"ghost-guri","text":"done","thread":"task-observed","mentions":["builder-lobo"]}}
{"id":83276,"ts":"2026-07-15T05:46:00Z","type":"message","data":{"from":"reviewer-meze","text":"seen","thread":"task-observed"}}
EOF
    ;;
  *) cat <<'EOF'
{"id":83270,"ts":"2026-07-15T05:40:00Z","type":"message","data":{"from":"reviewer-meze","text":"question"}}
{"id":83271,"ts":"2026-07-15T05:41:00Z","type":"message","data":{"from":"builder-lobo","text":"answer","reply_to_local":83270}}
{"id":83272,"ts":"2026-07-15T05:42:00Z","type":"message","data":{"from":"worker-suna","text":"plain"}}
{"id":83273,"ts":"2026-07-15T05:43:00Z","type":"message","data":{"from":"worker-kolu","text":"raise","thread":"task-x"}}
{"id":83274,"ts":"2026-07-15T05:44:00Z","type":"message","data":{"from":"human-yamen","text":"answer","thread":"task-x"}}
{"id":83275,"ts":"2026-07-15T05:45:00Z","type":"message","data":{"from":"ghost-guri","text":"done","thread":"task-observed"}}
{"id":83276,"ts":"2026-07-15T05:46:00Z","type":"message","data":{"from":"reviewer-meze","text":"seen","thread":"task-observed"}}
EOF
    ;;
esac
`, agentDir, agentDir, agentDir, agentDir, calls))
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	mish := writeExecutable(t, dir, "mish", "#!/bin/sh\nprintf '%s\\n' '{\"ok\":true,\"slug\":\"mission-one\"}'\n")
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, newMissionResolver(mish, ""))
	w.now = func() time.Time { return time.Date(2026, 7, 15, 5, 50, 0, 0, time.UTC) }

	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/graph?window=30m&view=map&focus=builder-lobo&auto=10", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("GET /graph = %d: %s", rw.Code, rw.Body.String())
	}
	body := rw.Body.String()
	for _, want := range []string{
		`<meta http-equiv="refresh" content="10">`, `<svg class="mc-graph"`, `mission: mission-one`,
		`desk @owner`, `human-yamen · your turn: 1`, `node ghost`, `ghost-guri`, `○ ghost`,
		`builder-lobo → reviewer-meze: 1`, `edge managed raise`, `desk driven by human-yamen`,
		`focus=reviewer-meze`, `as of 2026-07-15T05:50:00Z`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("graph page missing %q", want)
		}
	}
	if strings.Contains(strings.ToLower(body), "<script") {
		t.Fatal("graph page contains a script element")
	}
	if strings.Contains(body, "bigboss") {
		t.Fatal("implicit bigboss mention produced graph output")
	}
	if got := rw.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Fatalf("CSP = %q", got)
	}
	rawCalls, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(rawCalls)), "\n")
	if len(lines) != 2 {
		t.Fatalf("event query count = %d, want exactly 2: %s", len(lines), rawCalls)
	}
	if !strings.Contains(lines[0], "--type message --after 2026-07-15T05:20:00Z --last 500") || !strings.Contains(lines[1], "--mention owner") || !strings.Contains(lines[1], "--mention builder-lobo") {
		t.Fatalf("unexpected graph queries:\n%s", rawCalls)
	}

	// A second presentation reuses the cached model and exposes exact counts
	// and thread navigation without another shell-out.
	rw = httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/graph?view=matrix&window=30m", nil))
	body = rw.Body.String()
	for _, want := range []string{`class="graph-matrix"`, `from ↓ / to →`, `href="/thread/task-x"`, `href="/thread/task-observed"`, `@owner`} {
		if !strings.Contains(body, want) {
			t.Errorf("matrix page missing %q", want)
		}
	}
	rawCalls, _ = os.ReadFile(calls)
	if got := len(strings.Split(strings.TrimSpace(string(rawCalls)), "\n")); got != 2 {
		t.Fatalf("cached matrix triggered event queries; total = %d", got)
	}

	// URL-carried state composes: mission filtering retains the desk and the
	// controls retain mission/focus when toggling auto-refresh.
	rw = httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/graph?mission=mission-one&focus=builder-lobo&window=30m&view=map&auto=10", nil))
	body = rw.Body.String()
	for _, want := range []string{`mission: <strong>mission-one</strong>`, `desk @owner`, `href="/graph?focus=builder-lobo&amp;mission=mission-one&amp;view=map&amp;window=30m"`, `href="/graph?auto=10&amp;focus=builder-lobo&amp;mission=mission-one&amp;view=map&amp;window=30m"`} {
		if !strings.Contains(body, want) {
			t.Errorf("composed graph URL missing %q", want)
		}
	}
	if strings.Contains(body, "ghost-guri") {
		t.Fatal("mission filter retained a node outside the selected cluster")
	}
}

func TestGraphKeysLabeledRosterAgentByCanonicalAddress(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(dir, "agent")
	if err := os.Mkdir(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(dir, "event-calls")
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
if [ "$1" = list ]; then
  printf '%%s\n' '[{"name":"builder-nezi","base_name":"nezi","status":"active","directory":%q,"tool":"codex"}]'
  exit 0
fi
printf '%%s\n' "$*" >> %q
case " $* " in
  *" --mention "*) cat <<'EOF'
{"id":900,"ts":"2026-07-15T05:40:00Z","type":"message","data":{"from":"human-yamen","text":"question","mentions":["nezi"]}}
{"id":901,"ts":"2026-07-15T05:41:00Z","type":"message","data":{"from":"nezi","text":"answer","reply_to_local":900}}
EOF
    ;;
  *) cat <<'EOF'
{"id":900,"ts":"2026-07-15T05:40:00Z","type":"message","data":{"from":"human-yamen","text":"question"}}
{"id":901,"ts":"2026-07-15T05:41:00Z","type":"message","data":{"from":"nezi","text":"answer","reply_to_local":900}}
EOF
    ;;
esac
`, agentDir, calls))
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, nil)
	model := w.buildGraphModel("30m", time.Date(2026, 7, 15, 5, 50, 0, 0, time.UTC))

	var canonical *graphNode
	for i := range model.Nodes {
		switch model.Nodes[i].Name {
		case "nezi":
			canonical = &model.Nodes[i]
		case "builder-nezi":
			t.Fatalf("graph contains node keyed by full roster name: %#v", model.Nodes[i])
		}
	}
	if canonical == nil {
		t.Fatal("graph has no node keyed by canonical base name nezi")
	}
	if canonical.Kind != "agent" {
		t.Fatalf("canonical nezi node kind = %q, want agent (not ghost)", canonical.Kind)
	}

	var attached bool
	for _, edge := range model.Edges {
		if edge.A == "nezi" && edge.B == "owner" && edge.AB == 1 && edge.BA == 1 {
			attached = true
			break
		}
	}
	if !attached {
		t.Fatalf("canonical agent edge did not attach to nezi: %#v", model.Edges)
	}
	if svg := string(renderGraphSVG(model, "", "", "human-yamen", nil)); !strings.Contains(svg, "nezi → owner: 1") {
		t.Fatalf("rendered graph missing canonical nezi edge: %s", svg)
	}

	rawCalls, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	queries := string(rawCalls)
	if !strings.Contains(queries, "--mention nezi") {
		t.Fatalf("graph queries do not mention canonical base name:\n%s", queries)
	}
	if strings.Contains(queries, "--mention builder-nezi") {
		t.Fatalf("graph queries mention full roster name instead of base name:\n%s", queries)
	}
}

func TestGraphFailureRendersWarningAndPartialDataWithHTTP200(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	hcom := writeExecutable(t, dir, "hcom", `#!/bin/sh
if [ "$1" = list ]; then
  printf '%s\n' '[{"name":"builder-lobo","status":"active","directory":"/tmp"}]'
  exit 0
fi
printf '%s\n' 'bus offline' >&2
exit 1
`)
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", "/missing/herder", nil)
	w.now = func() time.Time { return time.Date(2026, 7, 15, 5, 50, 0, 0, time.UTC) }
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/graph", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("degraded graph = %d, want 200", rw.Code)
	}
	for _, want := range []string{"warning", "recent bus traffic", "bus offline", "builder-lobo", `<svg class="mc-graph"`} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Errorf("degraded graph missing %q", want)
		}
	}
}

func TestGraphDeterministicLayoutGolden(t *testing.T) {
	nodes := []graphNode{
		{Name: "builder-lobo", Kind: "agent", Mission: "mission-one", Activity: 4},
		{Name: "reviewer-meze", Kind: "agent", Mission: "mission-one", Activity: 2},
		{Name: "worker-kolu", Kind: "agent", Mission: "mission-two", Activity: 3},
		{Name: "ghost-guri", Kind: "ghost", Mission: "(no mission)", Activity: 1},
		{Name: "owner", Kind: "desk"},
	}
	height := layoutGraph(nodes)
	var got strings.Builder
	fmt.Fprintf(&got, "view-height %.0f\n", height)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	for _, node := range nodes {
		fmt.Fprintf(&got, "node %-13s %3.0f %3.0f %3.0f %2.0f\n", node.Name, node.X, node.Y, node.W, node.H)
	}
	for _, cluster := range graphClusters(nodes) {
		fmt.Fprintf(&got, "cluster %-12s %3.0f %3.0f %3.0f %3.0f\n", cluster.name, cluster.x, cluster.y, cluster.w, cluster.h)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "graph-layout.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		t.Fatalf("layout changed:\n--- got ---\n%s--- want ---\n%s", got.String(), want)
	}
}

func TestGraphEventTimeAcceptsHcomNaiveTimestamp(t *testing.T) {
	want := time.Date(2026, 7, 15, 5, 34, 14, 0, time.Local)
	if got := graphEventTime("2026-07-15T05:34:14", time.Time{}); !got.Equal(want) {
		t.Fatalf("naive hcom timestamp = %s, want %s", got, want)
	}
}

func TestTalkFormCarriesTargetInURLAndKeepsTitleVisible(t *testing.T) {
	_, s := testIngestor(t)
	w := NewWeb(s, &Bus{}, nil, "human-yamen", "owner", "", nil)
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/talk?agent=builder-dunu", nil))
	body := rw.Body.String()
	for _, want := range []string{`action="/talk?agent=builder-dunu"`, `name="message"`, `title (optional)`} {
		if !strings.Contains(body, want) {
			t.Errorf("talk form missing %q", want)
		}
	}
	for _, unwanted := range []string{`name="expects"`, `name="weight"`, `>Advanced<`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("talk form exposes agent-shaped field %q", unwanted)
		}
	}
	if strings.Contains(body, "Open a thread") || strings.Contains(body, "cold-open") {
		t.Fatal("talk form still exposes agent-grade open-thread language")
	}
}

func TestInboxShowsIngestStall(t *testing.T) {
	hcom, _ := fakePagedHcom(t, 1)
	s, err := OpenStore(filepath.Join(t.TempDir(), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.f.Close(); err != nil {
		t.Fatal(err)
	}
	in := NewIngestor(s, &Bus{Hcom: hcom}, "human-yamen", "owner")
	if err := in.Tick(); err == nil {
		t.Fatal("Tick succeeded with a closed journal")
	}

	w := NewWeb(s, &Bus{}, in, "human-yamen", "owner", "", nil)
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rw.Body.String()
	if !strings.Contains(body, "ingest stalled at #1 since ") || !strings.Contains(body, "file already closed") {
		t.Fatalf("root page missing ingest stall warning: %s", body)
	}
}

func TestHumanTalkDerivesMetadataAndSendsMessageVerbatim(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "journal.jsonl")
	s, err := OpenStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(dir, "agent")
	if err := os.Mkdir(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(dir, "send.txt")
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
if [ "$1" = list ]; then
  printf '%%s\n' '[{"name":"builder-dunu","status":"active","directory":%q}]'
  exit 0
fi
printf '%%s\n' "$@" > %q
`, agentDir, capture))
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	mish := writeExecutable(t, dir, "mish", "#!/bin/sh\nprintf '%s\\n' '{\"ok\":true,\"slug\":\"mission-one\"}'\n")
	bus := &Bus{Hcom: hcom}
	ing := NewIngestor(s, bus, "human-yamen", "owner")
	w := NewWeb(s, bus, ing, "human-yamen", "owner", herder, newMissionResolver(mish, ""))
	targets, home, err := w.resolveTalkTarget("mission", "mission-one")
	if err != nil || len(targets) != 1 || targets[0] != "builder-dunu" || home != "mission-one" {
		t.Fatalf("mission target = %v home %q err %v", targets, home, err)
	}

	message := "Please review this. Keep the response concise."
	form := url.Values{"message": {message}}
	req := httptest.NewRequest(http.MethodPost, "/talk?agent=builder-dunu", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, req)
	if rw.Code != http.StatusSeeOther {
		t.Fatalf("POST /talk = %d: %s", rw.Code, rw.Body.String())
	}
	id := strings.TrimPrefix(rw.Header().Get("Location"), "/thread/")
	th := s.Get(id)
	if th == nil {
		t.Fatal("talk did not create managed thread")
	}
	if th.Title != "Please review this" || th.Context != message || th.Expects != "reply" || th.Weight != "moment" {
		t.Fatalf("derived metadata = title %q context %q expects %q weight %q", th.Title, th.Context, th.Expects, th.Weight)
	}
	if th.Home != "mission-one" {
		t.Fatalf("home = %q, want target agent mission", th.Home)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if got := lines[len(lines)-1]; got != message {
		t.Fatalf("sent body = %q, want message verbatim", got)
	}
}

func TestHumanCloseDefaultsResolutionButAgentCloseStillRequiresOne(t *testing.T) {
	ing, s := testIngestor(t)
	if err := s.Open("managed-close", "close me", "ctx", "reply", "moment", "", "human-yamen", nil, "owner", "managed"); err != nil {
		t.Fatal(err)
	}

	postEmptyClose := func(user string) *httptest.ResponseRecorder {
		t.Helper()
		w := NewWeb(s, &Bus{}, ing, user, "owner", "", nil)
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodPost, "/thread/managed-close/close", nil))
		return rw
	}

	agent := postEmptyClose("builder-dunu")
	if got := agent.Header().Get("Location"); agent.Code != http.StatusSeeOther || !strings.Contains(got, "no+close+without+a+resolution") {
		t.Fatalf("agent close = %d Location %q, want missing-resolution refusal", agent.Code, got)
	}
	if got := s.Get("managed-close").Status; got != "open" {
		t.Fatalf("agent close changed status to %q, want open", got)
	}

	human := postEmptyClose("human-yamen")
	if got := human.Header().Get("Location"); human.Code != http.StatusSeeOther || got != "/" {
		t.Fatalf("human close = %d Location %q, want successful inbox redirect", human.Code, got)
	}
	thread := s.Get("managed-close")
	if thread.Status != "closed" || thread.Resolution != "closed by human-yamen" {
		t.Fatalf("human close = status %q resolution %q", thread.Status, thread.Resolution)
	}
}

func TestCloseNoticesOnlyLiveParticipantsAndSurfacesOrphans(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "sends.txt")
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
if [ "$1" = list ]; then
  printf '%%s\n' '[{"name":"builder-live","status":"listening"},{"name":"builder-dead","status":"inactive"}]'
  exit 0
fi
printf '%%s\n' "$*" >> %q
`, capture))
	ing, s := testIngestor(t)
	for _, id := range []string{"partly-live", "orphaned"} {
		with := []string{"builder-dead"}
		if id == "partly-live" {
			with = append(with, "builder-live")
		}
		if err := s.Open(id, id, "ctx", "reply", "moment", "", "human-yamen", with, strings.Join(with, ","), "managed"); err != nil {
			t.Fatal(err)
		}
	}
	w := NewWeb(s, &Bus{Hcom: hcom}, ing, "human-yamen", "owner", "", nil)

	for path, want := range map[string]string{
		"/thread/partly-live": "1 of 2 agent participants live",
		"/thread/orphaned":    "all agent participants gone",
	} {
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(rw.Body.String(), want) {
			t.Errorf("GET %s missing %q: %s", path, want, rw.Body.String())
		}
	}

	closeThread := func(id string) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{"resolution": {"done"}}
		req := httptest.NewRequest(http.MethodPost, "/thread/"+id+"/close", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, req)
		return rw
	}
	if got := closeThread("partly-live").Header().Get("Location"); got != "/" {
		t.Fatalf("partly-live close Location = %q", got)
	}
	if got := closeThread("orphaned").Header().Get("Location"); got != "/" {
		t.Fatalf("orphaned close Location = %q", got)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	sends := string(raw)
	if !strings.Contains(sends, "@builder-live") || strings.Contains(sends, "@builder-dead") || !strings.Contains(sends, "--thread orphaned") {
		t.Fatalf("filtered close sends = %q", sends)
	}
}

func TestReplyFiltersDeadParticipantsPostsAndJournals(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "sends.txt")
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
if [ "$1" = list ]; then
  printf '%%s\n' '[{"name":"builder-live","status":"active"},{"name":"builder-dead","status":"inactive"}]'
  exit 0
fi
printf '%%s\n' "$*" >> %q
`, capture))
	ing, s := testIngestor(t)
	if err := s.Open("mixed", "mixed", "ctx", "reply", "moment", "", "human-yamen", []string{"builder-live", "builder-dead"}, "builder-live,builder-dead", "managed"); err != nil {
		t.Fatal(err)
	}
	w := NewWeb(s, &Bus{Hcom: hcom}, ing, "human-yamen", "owner", "", nil)
	form := url.Values{"text": {"still here"}, "intent": {"inform"}}
	req := httptest.NewRequest(http.MethodPost, "/thread/mixed/reply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, req)
	if rw.Code != http.StatusSeeOther || rw.Header().Get("Location") != "/thread/mixed" {
		t.Fatalf("reply = %d Location %q", rw.Code, rw.Header().Get("Location"))
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if sent := string(raw); !strings.Contains(sent, "@builder-live") || strings.Contains(sent, "@builder-dead") || !strings.Contains(sent, "--thread mixed") {
		t.Fatalf("filtered reply send = %q", sent)
	}

	// The bus remains the message source of truth; folding its resulting event
	// journals the successful post without re-adding either agent alias.
	fold(t, ing, busEvent(101, "human-yamen", "mixed", "still here", "inform"))
	if got := s.Get("mixed"); len(got.Msgs) != 1 || got.Msgs[0].Text != "still here" {
		t.Fatalf("journaled thread = %#v", got)
	}
	rw = httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/thread/mixed", nil))
	if !strings.Contains(rw.Body.String(), "1 of 2 agent participants live") {
		t.Fatalf("thread missing dead-member state: %s", rw.Body.String())
	}
}

func TestReplyAllDeadStillPostsThreadOnly(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "sends.txt")
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
if [ "$1" = list ]; then
  printf '%%s\n' '[{"name":"builder-dead","status":"inactive"}]'
  exit 0
fi
printf '%%s\n' "$*" >> %q
`, capture))
	ing, s := testIngestor(t)
	if err := s.Open("orphan", "orphan", "ctx", "reply", "moment", "", "human-yamen", []string{"builder-dead"}, "builder-dead", "managed"); err != nil {
		t.Fatal(err)
	}
	w := NewWeb(s, &Bus{Hcom: hcom}, ing, "human-yamen", "owner", "", nil)
	form := url.Values{"text": {"record this"}}
	req := httptest.NewRequest(http.MethodPost, "/thread/orphan/reply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, req)
	if rw.Code != http.StatusSeeOther || rw.Header().Get("Location") != "/thread/orphan" {
		t.Fatalf("all-dead reply = %d Location %q", rw.Code, rw.Header().Get("Location"))
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if sent := string(raw); !strings.Contains(sent, "@owner") || strings.Contains(sent, "@builder-dead") || !strings.Contains(sent, "--thread orphan") || !strings.Contains(sent, "record this") {
		t.Fatalf("seat-addressed thread reply = %q", sent)
	}
}

func TestReplyAllDeadPostsViaSeatWithRealHcom(t *testing.T) {
	hcom, err := exec.LookPath("hcom")
	if err != nil {
		t.Skip("real hcom binary not available")
	}

	bus := &Bus{Hcom: hcom, Dir: t.TempDir()}
	if err := bus.EnsureSeat("owner"); err != nil {
		t.Fatalf("register owner seat: %v", err)
	}
	if _, err := bus.run("start", "--as", "builder-dead"); err != nil {
		t.Fatalf("register thread member: %v", err)
	}
	if err := bus.Send("human-yamen", []string{"builder-dead"}, "all-dead-contract", "", "inform", "seed"); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	if _, err := bus.run("stop", "builder-dead"); err != nil {
		t.Fatalf("stop thread member: %v", err)
	}

	w := NewWeb(nil, bus, nil, "human-yamen", "owner", "", nil)
	if _, err := w.sendToThread("human-yamen", []string{"builder-dead"}, "all-dead-contract", "inform", "after everyone left"); err != nil {
		t.Fatalf("all-dead reply: %v", err)
	}

	out, err := bus.run("events", "--thread", "all-dead-contract", "--mention", "owner", "--last", "20")
	if err != nil {
		t.Fatalf("read thread events: %v", err)
	}
	found := false
	for _, ev := range parseBusEvents(out) {
		if ev.Data.From == "human-yamen" && ev.Data.Thread == "all-dead-contract" && ev.Data.Text == "after everyone left" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("seat-addressed reply was not posted to thread:\n%s", out)
	}
}

func TestSendRaceFallsBackToThreadOnly(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "sends.txt")
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
if [ "$1" = list ]; then
  printf '%%s\n' '[{"name":"builder-live","status":"active"}]'
  exit 0
fi
printf '%%s\n' "$*" >> %q
case "$*" in
  *"@builder-live"*) printf '%%s\n' 'agent stopped during send' >&2; exit 1 ;;
esac
`, capture))
	w := NewWeb(nil, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", "", nil)
	warn, err := w.sendToThread("human-yamen", []string{"builder-live"}, "race", "inform", "land anyway")
	if err != nil || !strings.Contains(warn, "changed before send") {
		t.Fatalf("race fallback warning %q err %v", warn, err)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if sends := strings.TrimSpace(string(raw)); strings.Count(sends, "\n") != 1 || !strings.Contains(sends, "@builder-live") || !strings.Contains(sends, "@owner") || !strings.Contains(sends, "--thread race") {
		t.Fatalf("race sends = %q, want addressed attempt plus seat-addressed fallback", sends)
	}
}

func TestRosterUsesCanonicalBusAddressWithoutDuplicateParticipant(t *testing.T) {
	dir := t.TempDir()
	hcom := writeExecutable(t, dir, "hcom", `#!/bin/sh
printf '%s\n' '[{"name":"builder-beta","base_name":"beta","status":"inactive"}]'
`)
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	_, s := testIngestor(t)
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, nil)
	targets, _, err := w.resolveTalkTarget("agent", "builder-beta")
	if err != nil || len(targets) != 1 || targets[0] != "beta" {
		t.Fatalf("canonical target = %v err %v, want [beta]", targets, err)
	}
	if err := s.Open("aliases", "aliases", "ctx", "reply", "moment", "", "human-yamen", targets, "beta", "managed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Link("aliases", Msg{BusID: 1, From: "beta", Text: "reply"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("aliases").With; len(got) != 1 || got[0] != "beta" {
		t.Fatalf("participants = %v, want one canonical bus name", got)
	}

	canonical := canonicalParticipantTargets([]string{"builder-beta", "beta"}, []BusAgent{{Name: "builder-beta", BaseName: "beta", Status: "active"}})
	if len(canonical.wanted) != 1 || len(canonical.live) != 1 || canonical.live[0] != "beta" {
		t.Fatalf("canonicalized legacy aliases = %#v", canonical)
	}
}

func TestCloseReportsFailureForRecipientConfirmedLive(t *testing.T) {
	dir := t.TempDir()
	hcom := writeExecutable(t, dir, "hcom", `#!/bin/sh
if [ "$1" = list ]; then
  printf '%s\n' '[{"name":"builder-live","status":"active"}]'
  exit 0
fi
printf '%s\n' 'live delivery broke' >&2
exit 1
`)
	ing, s := testIngestor(t)
	if err := s.Open("live-failure", "live failure", "ctx", "reply", "moment", "", "human-yamen", []string{"builder-live"}, "builder-live", "managed"); err != nil {
		t.Fatal(err)
	}
	w := NewWeb(s, &Bus{Hcom: hcom}, ing, "human-yamen", "owner", "", nil)
	form := url.Values{"resolution": {"done"}}
	req := httptest.NewRequest(http.MethodPost, "/thread/live-failure/close", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, req)
	if got := rw.Header().Get("Location"); !strings.Contains(got, "closed%2C+but+the+bus+notice+failed") {
		t.Fatalf("live delivery failure Location = %q", got)
	}
}

func TestRosterHygieneAndMissionlessRepoBranchGrouping(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "parent-repo")
	worktree := filepath.Join(dir, "feature-worktree")
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=MC Test", "GIT_AUTHOR_EMAIL=mc@example.test", "GIT_COMMITTER_NAME=MC Test", "GIT_COMMITTER_EMAIL=mc@example.test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	runGit("init", "-b", "main", repo)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("-C", repo, "add", "README.md")
	runGit("-C", repo, "commit", "-m", "fixture")
	runGit("-C", repo, "worktree", "add", "-b", "feature", worktree)

	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '[{"name":"owner","base_name":"owner","status":"listening","directory":%q},{"name":"builder-main","status":"active","session_id":"sid-main","directory":%q},{"name":"builder-feature","status":"listening","session_id":"sid-feature","directory":%q},{"name":"unmanaged-husk","status":"active","directory":%q}]'
`, repo, repo, worktree, repo))
	herder := writeExecutable(t, dir, "herder", fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"kind":"session","label":"builder-main","guid":"g-main","tool":"codex","role":"builder","state":"seated","provenance":{"cwd":%q,"branch":"main","tool_session_id":"sid-main"}}'
printf '%%s\n' '{"kind":"session","label":"builder-feature","guid":"g-feature","tool":"codex","role":"builder","state":"seated","provenance":{"cwd":%q,"branch":"feature","tool_session_id":"sid-feature"}}'
`, repo, worktree))
	mish := writeExecutable(t, dir, "mish", "#!/bin/sh\nprintf '%s\\n' '{\"ok\":false}'\nexit 1\n")
	_, s := testIngestor(t)
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, newMissionResolver(mish, ""))

	groups, errText := w.rosterGroups(false)
	if errText != "" {
		t.Fatalf("roster error = %q", errText)
	}
	if len(groups) != 1 || groups[0].Dir != "no mission" || len(groups[0].Repos) != 1 {
		t.Fatalf("missionless groups = %#v", groups)
	}
	rg := groups[0].Repos[0]
	if rg.Repo != "parent-repo" || len(rg.Branches) != 2 || rg.Branches[0].Branch != "feature" || rg.Branches[1].Branch != "main" {
		t.Fatalf("repo/branch grouping = %#v", rg)
	}
	var unmanagedFound, ownerFound bool
	for _, branch := range rg.Branches {
		for _, a := range branch.Agents {
			ownerFound = ownerFound || a.Name == "owner"
			unmanagedFound = unmanagedFound || (a.Name == "unmanaged-husk" && a.Unmanaged)
		}
	}
	if ownerFound || !unmanagedFound {
		t.Fatalf("ownerFound=%v unmanagedFound=%v", ownerFound, unmanagedFound)
	}

	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/roster", nil))
	body := rw.Body.String()
	for _, want := range []string{"<h2>no mission</h2>", "repo: parent-repo", "branch: feature", "branch: main", "unmanaged-husk", "unmanaged"} {
		if !strings.Contains(body, want) {
			t.Errorf("roster missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "<strong>owner</strong>") {
		t.Fatal("roster rendered mc's own seat")
	}
}

func TestRetitleIsManagedOnlyAndReplays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open("managed", "placeholder", "ctx", "reply", "moment", "", "human-yamen", []string{"vile"}, "vile", "managed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Open("observed", "hands off", "", "read", "moment", "", "vile", nil, "vile", "observed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Retitle("managed", "Durable title"); err != nil {
		t.Fatal(err)
	}
	if err := s.Retitle("observed", "bypass"); err == nil {
		t.Fatal("observed retitle was accepted")
	}

	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get("managed").Title; got != "Durable title" {
		t.Fatalf("replayed title = %q", got)
	}
	w := NewWeb(s2, &Bus{}, nil, "human-yamen", "owner", "", nil)
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/thread/observed", nil))
	if strings.Contains(rw.Body.String(), "/retitle") {
		t.Fatal("observed thread page exposes retitle")
	}
}

func TestMissionPageRendersStatusWarningsThreadsAndRoster(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "journal.jsonl")
	s, err := OpenStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, grade string }{{"managed-thread", "managed"}, {"observed-thread", "observed"}} {
		if err := s.Open(tc.id, tc.id+" title", "ctx", "reply", "moment", "mission-one", "vile", nil, "vile", tc.grade); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Open("other", "other title", "ctx", "reply", "moment", "mission-two", "vile", nil, "vile", "managed"); err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(dir, "agent")
	if err := os.Mkdir(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '[{"name":"builder-dunu","status":"active","directory":%q,"tool":"codex"}]'
`, agentDir))
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
if [ "$1" = "resolve" ]; then
  printf '%%s\n' '{"ok":true,"slug":"mission-one"}'
else
  cat %q
fi
`, mishFixture(t, "status-mission.json")))
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, newMissionResolver(mish, ""))
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/mission/mission-one", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("GET mission = %d: %s", rw.Code, rw.Body.String())
	}
	body := rw.Body.String()
	for _, want := range []string{
		"Goal / manifest", "authority hera", "owner riley", "/missions-repo/missions/mission-one",
		"Board", "To Do 1", "TASK-1", "First task", "artifacts missing: artifacts/", "ui", "high",
		"managed-thread title", "observed-thread title", "managed", "observed",
		"Seated agents", "builder-dunu", "/talk?mission=mission-one",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("mission page missing %q", want)
		}
	}
	if strings.Contains(body, "other title") {
		t.Fatal("mission page included a thread homed on another mission")
	}
}

func TestMissionPageRendersTypedRefusalAsWarning(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
cat %q
exit 1
`, mishFixture(t, "status-mission-not-found.json")))
	w := NewWeb(s, &Bus{Hcom: "/missing/hcom"}, nil, "human-yamen", "owner", "/missing/herder", newMissionResolver(mish, ""))
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/mission/ghost", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("typed refusal status = %d, want 200", rw.Code)
	}
	for _, want := range []string{"Mission data unavailable", "mission_not_found", "mission ghost not found", "check the slug or create the mission", "board unavailable"} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Errorf("refusal page missing %q", want)
		}
	}
}

func TestMissionEntryPointsLinkToMissionPage(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(dir, "agent")
	if err := os.Mkdir(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '[{\"name\":\"builder-dunu\",\"status\":\"active\",\"directory\":%q}]'\n", agentDir))
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
if [ "$1" = "resolve" ]; then
  printf '%%s\n' '{"ok":true,"slug":"mission-one"}'
elif [ "$3" = "--all" ] || [ "$2" = "--all" ]; then
  cat %q
else
  exit 1
fi
`, mishFixture(t, "status-all.json")))
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, newMissionResolver(mish, ""))
	for _, path := range []string{"/", "/roster"} {
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(rw.Body.String(), `href="/mission/mission-one"`) {
			t.Errorf("%s missing mission-page entry point: %s", path, rw.Body.String())
		}
		if path == "/" {
			for _, want := range []string{`href="/mission/mission-broken"`, "degraded", "artifacts missing: artifacts/", `href="/mission/mission-one"`} {
				if !strings.Contains(rw.Body.String(), want) {
					t.Errorf("root missing isolated degraded card %q", want)
				}
			}
			if strings.Contains(rw.Body.String(), "board missing: backlog/config.yml") {
				t.Error("degraded card rendered more than its first warning")
			}
		}
	}
}

func TestMissionListRendersTypedObjectRefusal(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf("#!/bin/sh\ncat %q\nexit 1\n", mishFixture(t, "status-repo-unset.json")))
	w := NewWeb(s, &Bus{}, nil, "human-yamen", "owner", "", newMissionResolver(mish, ""))
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("GET / = %d: %s", rw.Code, rw.Body.String())
	}
	for _, want := range []string{"missions_repo_unset", "$MISSIONS_REPO is not set", "set MISSIONS_REPO to the shared missions repo"} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Errorf("mission-list refusal missing %q: %s", want, rw.Body.String())
		}
	}
}

func mishFixture(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
