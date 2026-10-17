package servecmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func postLane(t *testing.T, deps dependencies, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, request)
	return response
}

func TestLaneEndpointsAppendTheCLIEvents(t *testing.T) {
	deps := supervisionDeps(t)
	response := postLane(t, deps, "/api/lanes", `{"name":" swap ","lead":"sesh-nabi"}`)
	var created laneResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil || response.Code != http.StatusOK || created.ID == "" {
		t.Fatalf("new: status=%d id=%q err=%v", response.Code, created.ID, err)
	}
	if response := postLane(t, deps, "/api/lanes/"+created.ID, `{"name":"swap-perf"}`); response.Code != http.StatusOK {
		t.Fatalf("rename: status=%d body=%s", response.Code, response.Body.String())
	}
	if response := postLane(t, deps, "/api/lanes/"+created.ID, `{"lead":"sesh-kele"}`); response.Code != http.StatusOK {
		t.Fatalf("lead: status=%d body=%s", response.Code, response.Body.String())
	}
	proj, err := deps.store.Replay()
	if err != nil {
		t.Fatal(err)
	}
	lane := proj.Lanes[created.ID]
	if lane == nil || lane.Name != "swap-perf" || lane.Lead != "sesh-kele" || strings.Join(lane.Names, ",") != "swap" {
		t.Fatalf("lane = %+v", lane)
	}
	if proj.Latest("sesh-nabi").LaneID != created.ID || proj.Latest("sesh-kele").LaneID != created.ID {
		t.Fatal("new and lead did not move the seats into the lane")
	}
	updated := proj.Latest("sesh-kele").Events
	if last := updated[len(updated)-1]; last.By != "web-alice-example-com" || last.ByKind != "web" {
		t.Fatalf("attribution = %+v", last)
	}
}

func TestLaneEndpointsRefuse(t *testing.T) {
	deps := supervisionDeps(t)
	before, err := deps.store.Replay()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path, body string
		code       int
	}{
		{"/api/lanes", `{"name":"x"}`, http.StatusBadRequest},
		{"/api/lanes", `{"name":"payload","lead":"sesh-nabi"}`, http.StatusBadRequest}, // name taken by a live lane
		{"/api/lanes", `{"name":"--all","lead":"sesh-nabi"}`, http.StatusBadRequest},
		{"/api/lanes/" + supervisionLane, `{}`, http.StatusBadRequest},
		{"/api/lanes/" + supervisionLane, `{"name":"a","lead":"ziru"}`, http.StatusBadRequest},
		{"/api/lanes/nope", `{"name":"a"}`, http.StatusNotFound},
	} {
		if response := postLane(t, deps, c.path, c.body); response.Code != c.code {
			t.Fatalf("%s %s: status=%d body=%s", c.path, c.body, response.Code, response.Body.String())
		}
	}
	after, err := deps.store.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if after.EventsOffset != before.EventsOffset {
		t.Fatal("a refused lane request wrote to the store")
	}
}

func TestLaneMovesServesTheMoveFile(t *testing.T) {
	deps := supervisionDeps(t)
	get := func() (int, map[string]string) {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/lanes/moves", nil))
		moves := map[string]string{}
		_ = json.NewDecoder(response.Body).Decode(&moves)
		return response.Code, moves
	}
	if code, moves := get(); code != http.StatusOK || len(moves) != 0 {
		t.Fatalf("absent: %d %v", code, moves)
	}
	path := filepath.Join(filepath.Dir(deps.store.Dir), laneMoveFile)
	if err := os.WriteFile(path, []byte(`{"space-1":"`+supervisionLane+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, moves := get(); code != http.StatusOK || moves["space-1"] != supervisionLane {
		t.Fatalf("present: %d %v", code, moves)
	}
}
