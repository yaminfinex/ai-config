package servecmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/webstate"
)

func stateFixture(t *testing.T) dependencies {
	t.Helper()
	store, err := webstate.NewFileStore(t.TempDir(), webstate.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	deps := fixtureDeps()
	deps.state = store
	deps.stateChanges = newStateChangeBroker()
	return deps
}

func TestGenericStateEndpointRoundTripsSpacesAndASecondNamespace(t *testing.T) {
	deps := stateFixture(t)
	handler := newHandler(deps)
	for _, namespace := range []string{"spaces", "notes"} {
		body := fmt.Sprintf(`{"rows":[{"key":"%s-1","value":{"name":"one"},"updated":1,"writeID":"device-a","deleted":false}]}`, namespace)
		post := httptest.NewRecorder()
		handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/state/"+namespace, strings.NewReader(body)))
		if post.Code != http.StatusOK {
			t.Fatalf("POST %s status=%d body=%s", namespace, post.Code, post.Body.String())
		}
		var pushed struct {
			Accepted []string `json:"accepted"`
			Rev      uint64   `json:"rev"`
		}
		if err := json.Unmarshal(post.Body.Bytes(), &pushed); err != nil || len(pushed.Accepted) != 1 || pushed.Rev != 1 {
			t.Fatalf("POST %s response=%+v err=%v", namespace, pushed, err)
		}

		get := httptest.NewRecorder()
		handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/state/"+namespace+"?since=0", nil))
		if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), namespace+`-1`) {
			t.Fatalf("GET %s status=%d body=%s", namespace, get.Code, get.Body.String())
		}
	}

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/state/session.annotations?since=0", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing namespace status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestStatePOSTIsIdempotentAndRejectsUnattributedWritesWithComposer409(t *testing.T) {
	deps := stateFixture(t)
	handler := newHandler(deps)
	body := `{"rows":[{"key":"main","value":{"name":"main"},"updated":1,"writeID":"same","deleted":false}]}`
	for index, wantAccepted := range []bool{true, false} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/state/spaces", strings.NewReader(body)))
		if response.Code != http.StatusOK {
			t.Fatalf("POST %d status=%d body=%s", index, response.Code, response.Body.String())
		}
		accepted := strings.Contains(response.Body.String(), `"main"`)
		if accepted != wantAccepted {
			t.Fatalf("POST %d accepted=%v want=%v body=%s", index, accepted, wantAccepted, response.Body.String())
		}
	}

	deps.sender = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("peer not found")
	}
	refused := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(refused, httptest.NewRequest(http.MethodPost, "/api/state/spaces", strings.NewReader(body)))
	if refused.Code != http.StatusConflict || !strings.Contains(refused.Body.String(), "attribution required") {
		t.Fatalf("unattributed status=%d body=%s", refused.Code, refused.Body.String())
	}
}

func TestStateEndpointPinsRowValueRowCountAndExistingBodyBoundsAs413(t *testing.T) {
	store, err := webstate.NewFileStore(t.TempDir(), webstate.Limits{MaxValueBytes: 8, MaxRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	deps := fixtureDeps()
	deps.state = store
	deps.stateChanges = newStateChangeBroker()
	handler := newHandler(deps)
	for name, body := range map[string]string{
		"value": `{"rows":[{"key":"too-large","value":"123456789","updated":1,"writeID":"a","deleted":false}]}`,
		"count": `{"rows":[{"key":"a","value":1,"updated":1,"writeID":"a","deleted":false},{"key":"b","value":2,"updated":1,"writeID":"b","deleted":true}]}`,
		"body":  `{"rows":[{"key":"body","value":"` + strings.Repeat("x", (64<<10)+1) + `","updated":1,"writeID":"a","deleted":false}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/state/spaces", strings.NewReader(body)))
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestStateChangeRidesTheExistingEventStream(t *testing.T) {
	deps := stateFixture(t)
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()

	events, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer events.Body.Close()
	reader := bufio.NewReader(events.Body)
	event, _ := readEvent(t, reader)
	if event != "hello" {
		t.Fatalf("first event=%q", event)
	}

	body := []byte(`{"rows":[{"key":"main","value":{"name":"main"},"updated":1,"writeID":"device","deleted":false}]}`)
	response, err := http.Post(server.URL+"/api/state/spaces", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST status=%d", response.StatusCode)
	}

	deadline := time.After(2 * time.Second)
	for {
		result := make(chan struct {
			event string
			data  string
		}, 1)
		go func() {
			event, data := readEvent(t, reader)
			result <- struct {
				event string
				data  string
			}{event, data}
		}()
		select {
		case got := <-result:
			if got.event == "state-changed" {
				if !strings.Contains(got.data, `"namespace":"spaces"`) || !strings.Contains(got.data, `"rev":1`) {
					t.Fatalf("state event data=%s", got.data)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for state-changed on /api/events")
		}
	}
}

func TestUnavailableStateStoreDoesNotTakeDownFleetAndReturns503(t *testing.T) {
	deps := fixtureDeps()
	deps.state = webstate.Unavailable(errors.New("scratch state directory is read-only"))
	deps.stateChanges = newStateChangeBroker()
	handler := newHandler(deps)
	fleet := httptest.NewRecorder()
	handler.ServeHTTP(fleet, httptest.NewRequest(http.MethodGet, "/api/fleet", nil))
	if fleet.Code != http.StatusOK {
		t.Fatalf("fleet status=%d body=%s", fleet.Code, fleet.Body.String())
	}
	state := httptest.NewRecorder()
	handler.ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/api/state/spaces", nil))
	if state.Code != http.StatusServiceUnavailable || !strings.Contains(state.Body.String(), "read-only") {
		t.Fatalf("state status=%d body=%s", state.Code, state.Body.String())
	}
}
