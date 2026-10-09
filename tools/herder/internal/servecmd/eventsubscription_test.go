package servecmd

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomidentity"
)

// subscriptionFixture serves dore and kumo; a transcript yields one new
// entry per pending mark, and every tail and end read is counted.
type subscriptionFixture struct {
	mu      sync.Mutex
	pending map[string]int
	tails   map[string]int
	ends    map[string]int
}

func (f *subscriptionFixture) mark(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending[name]++
}

func (f *subscriptionFixture) count(counts map[string]int, name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return counts[name]
}

func newSubscriptionServer(t *testing.T) (*httptest.Server, *subscriptionFixture) {
	t.Helper()
	fixture := &subscriptionFixture{pending: map[string]int{}, tails: map[string]int{}, ends: map[string]int{}}
	deps := fixtureDeps()
	deps.transcriptSafety = 10 * time.Millisecond
	deps.screens = func() (screenSource, error) { return &fixtureScreenSource{text: "screen text"}, nil }
	baseRoster := deps.roster
	deps.roster = func() ([]hcomidentity.Row, error) {
		rows, _ := baseRoster()
		return append(rows, hcomidentity.Row{Name: "kumo", Tool: "claude", Status: "active", SessionID: "session-kumo"}), nil
	}
	deps.entryEnd = func(row hcomidentity.Row) (int64, error) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		fixture.ends[row.Name]++
		return 0, nil
	}
	deps.entryTail = func(row hcomidentity.Row, cursor claudesession.Cursor, _ int) (claudesession.TailResult, error) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		fixture.tails[row.Name]++
		result := claudesession.TailResult{Cursor: claudesession.Cursor{SessionID: row.SessionID, Offset: cursor.Offset}}
		if fixture.pending[row.Name] > 0 {
			fixture.pending[row.Name]--
			result.Read = claudesession.ReadResult{
				Entries:    []claudesession.Entry{{UUID: "invented-" + row.Name, Line: 1, ByteOffset: cursor.Offset, Timestamp: "2026-01-02T03:04:05Z", Kind: claudesession.KindAssistantText, Payload: json.RawMessage(`{"invented":true}`)}},
				NextOffset: cursor.Offset + 10,
			}
			result.Cursor.Offset = cursor.Offset + 10
		}
		return result, nil
	}
	server := httptest.NewServer(newHandler(deps))
	t.Cleanup(server.Close)
	return server, fixture
}

func openSubscribedStream(t *testing.T, server *httptest.Server, query string) (*bufio.Reader, string) {
	t.Helper()
	response, err := http.Get(server.URL + "/api/events?" + query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	reader := bufio.NewReader(response.Body)
	event, data := readEvent(t, reader)
	var hello streamHello
	if event != "hello" || json.Unmarshal([]byte(data), &hello) != nil || hello.Stream == "" {
		t.Fatalf("hello = %q %s", event, data)
	}
	return reader, hello.Stream
}

func postSubscription(t *testing.T, server *httptest.Server, query url.Values) int {
	t.Helper()
	response, err := http.Post(server.URL+"/api/events/subscription?"+query.Encode(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return response.StatusCode
}

// readUntil reads events until want arrives, returning its data and every event name seen before it.
func readUntil(t *testing.T, reader *bufio.Reader, want string) (string, []string) {
	t.Helper()
	seen := []string{}
	for range 200 {
		event, data := readEvent(t, reader)
		if event == want {
			return data, seen
		}
		seen = append(seen, event)
	}
	t.Fatalf("no %s event; saw %v", want, seen)
	return "", nil
}

func TestSubscriptionUpdateRetargetsTheLiveStream(t *testing.T) {
	server, fixture := newSubscriptionServer(t)
	reader, stream := openSubscribedStream(t, server, "agents=dore")
	if data, _ := readUntil(t, reader, "subscribed"); data != `{"agents":["dore"]}` {
		t.Fatalf("initial subscribed = %s", data)
	}

	// Adding kumo captures its offset before the update answers, then announces it.
	if status := postSubscription(t, server, url.Values{"stream": {stream}, "agents": {"dore,kumo"}, "screens": {"p1"}, "focused_screen": {"p1"}}); status != http.StatusNoContent {
		t.Fatalf("add status = %d", status)
	}
	if fixture.count(fixture.ends, "kumo") != 1 {
		t.Fatalf("kumo offset captures = %d", fixture.count(fixture.ends, "kumo"))
	}
	if data, _ := readUntil(t, reader, "subscribed"); data != `{"agents":["kumo"]}` {
		t.Fatalf("added subscribed = %s", data)
	}
	if data, _ := readUntil(t, reader, "screen:p1"); !strings.Contains(data, "screen text") {
		t.Fatalf("added screen frame = %s", data)
	}
	fixture.mark("kumo")
	readUntil(t, reader, "entry:kumo")

	// Dropping dore stops its tails on the same connection.
	if status := postSubscription(t, server, url.Values{"stream": {stream}, "agents": {"kumo"}}); status != http.StatusNoContent {
		t.Fatalf("remove status = %d", status)
	}
	doreTails := fixture.count(fixture.tails, "dore")
	fixture.mark("dore")
	fixture.mark("kumo")
	if _, seen := readUntil(t, reader, "entry:kumo"); strings.Contains(strings.Join(seen, ","), "entry:dore") || strings.Contains(strings.Join(seen, ","), "subscribed") {
		t.Fatalf("events after dropping dore: %v", seen)
	}
	time.Sleep(50 * time.Millisecond)
	if fixture.count(fixture.tails, "dore") != doreTails {
		t.Fatalf("dore tailed after unsubscribe: %d -> %d", doreTails, fixture.count(fixture.tails, "dore"))
	}
	if fixture.count(fixture.ends, "dore") != 1 || fixture.count(fixture.ends, "kumo") != 1 {
		t.Fatalf("an update re-read kept offsets: ends=%v", fixture.ends)
	}
}

func TestSubscriptionUpdateRefusesUnknownStreamsAndBadSubscriptions(t *testing.T) {
	server, _ := newSubscriptionServer(t)
	_, stream := openSubscribedStream(t, server, "agents=dore")
	if status := postSubscription(t, server, url.Values{"stream": {"not-a-stream"}, "agents": {"dore"}}); status != http.StatusNotFound {
		t.Fatalf("unknown stream status = %d", status)
	}
	if status := postSubscription(t, server, url.Values{"stream": {stream}, "screens": {"p1"}, "focused_screen": {"p2"}}); status != http.StatusBadRequest {
		t.Fatalf("bad focus status = %d", status)
	}
	response, err := http.Get(server.URL + "/api/events/subscription?stream=" + stream)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET status = %d", response.StatusCode)
	}
}

func TestSubscriptionUpdateAfterTheStreamClosesIsUnknown(t *testing.T) {
	server, _ := newSubscriptionServer(t)
	response, err := http.Get(server.URL + "/api/events?agents=dore")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	_, data := readEvent(t, reader)
	var hello streamHello
	if err := json.Unmarshal([]byte(data), &hello); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		status := postSubscription(t, server, url.Values{"stream": {hello.Stream}, "agents": {"kumo"}})
		if status == http.StatusNotFound {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("closed stream still takes updates: %d", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A new stream's first board reads the poll-fresh cached roster when it holds
// every subscribed agent; one that lacks a subscribed agent asks hcom live.
func TestFirstBoardReadsTheFreshRosterHoldingItsAgents(t *testing.T) {
	deps := fixtureDeps()
	deps.poll = time.Hour
	base := deps.roster
	var mu sync.Mutex
	asks := 0
	deps.roster = func() ([]hcomidentity.Row, error) {
		mu.Lock()
		asks++
		mu.Unlock()
		return base()
	}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return asks
	}
	rows, _ := base()
	deps.rosterCache.set(rows)
	server := httptest.NewServer(newHandler(deps))
	t.Cleanup(server.Close)

	reader, _ := openSubscribedStream(t, server, "agents=dore")
	readUntil(t, reader, "fleet")
	if count() != 0 {
		t.Fatalf("first board holding dore asked hcom %d times", count())
	}
	reader, _ = openSubscribedStream(t, server, "agents=dore,kumo")
	readUntil(t, reader, "fleet")
	if count() != 1 {
		t.Fatalf("first board lacking kumo asked hcom %d times, want 1", count())
	}
}
