package servecmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"
)

// eventSubscription is what one SSE connection follows: transcripts by
// agent, screen frames by pane, file watches, and the focused screen. The
// connection URL carries the first one; a subscription update replaces it
// on the live connection so a space switch never reopens the stream.
type eventSubscription struct {
	agents        []string
	screens       []string
	focusedScreen string
	watches       []fileWatchRequest
}

func optionalValue(query url.Values, name string) (string, error) {
	values, present := query[name]
	if !present {
		return "", nil
	}
	if len(values) != 1 {
		return "", fmt.Errorf("query parameter %q may appear at most once", name)
	}
	return values[0], nil
}

func parseEventSubscription(query url.Values) (eventSubscription, error) {
	agents, err := eventAgents(query.Get("agents"))
	if err != nil {
		return eventSubscription{}, err
	}
	screens, err := eventSet(query.Get("screens"), "screens")
	if err != nil {
		return eventSubscription{}, err
	}
	focusedScreen, err := optionalValue(query, "focused_screen")
	if err != nil {
		return eventSubscription{}, err
	}
	if focusedScreen != "" && !slices.Contains(screens, focusedScreen) {
		return eventSubscription{}, errors.New("focused_screen must name one of the requested screens")
	}
	watchesRaw, err := optionalValue(query, "watches")
	if err != nil {
		return eventSubscription{}, err
	}
	watches, err := parseFileWatchRequests(watchesRaw)
	if err != nil {
		return eventSubscription{}, err
	}
	return eventSubscription{agents: agents, screens: screens, focusedScreen: focusedScreen, watches: watches}, nil
}

// subscriptionUpdate hands a new subscription to the connection's loop;
// applied is closed once the loop has captured the new transcripts' offsets.
type subscriptionUpdate struct {
	subscription eventSubscription
	applied      chan struct{}
}

type liveStream struct {
	updates chan subscriptionUpdate
	done    chan struct{}
}

// eventStreams finds a live SSE connection by the id its hello announced.
type eventStreams struct {
	mu      sync.Mutex
	streams map[string]liveStream
}

func newEventStreams() *eventStreams {
	return &eventStreams{streams: map[string]liveStream{}}
}

func (s *eventStreams) register() (string, <-chan subscriptionUpdate, func()) {
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	id := hex.EncodeToString(raw)
	stream := liveStream{updates: make(chan subscriptionUpdate), done: make(chan struct{})}
	s.mu.Lock()
	s.streams[id] = stream
	s.mu.Unlock()
	return id, stream.updates, func() {
		s.mu.Lock()
		delete(s.streams, id)
		s.mu.Unlock()
		close(stream.done)
	}
}

func (s *eventStreams) lookup(id string) (liveStream, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stream, ok := s.streams[id]
	return stream, ok
}

// subscriptionUpdateWait bounds how long an update waits for a connection
// still building its first board.
const subscriptionUpdateWait = 15 * time.Second

func serveEventSubscription(w http.ResponseWriter, r *http.Request, streams *eventStreams) {
	if r.Method != http.MethodPost {
		refuse(w, http.StatusBadRequest, "bad request", "POST required")
		return
	}
	query := r.URL.Query()
	stream, ok := streams.lookup(query.Get("stream"))
	if !ok {
		refuse(w, http.StatusNotFound, "unknown stream", "no live event stream has this id; reconnect")
		return
	}
	subscription, err := parseEventSubscription(query)
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), subscriptionUpdateWait)
	defer cancel()
	update := subscriptionUpdate{subscription: subscription, applied: make(chan struct{})}
	select {
	case stream.updates <- update:
	case <-stream.done:
		refuse(w, http.StatusNotFound, "unknown stream", "the event stream closed; reconnect")
		return
	case <-ctx.Done():
		refuse(w, http.StatusServiceUnavailable, "stream busy", "the event stream did not take the update; reconnect")
		return
	}
	select {
	case <-update.applied:
		w.WriteHeader(http.StatusNoContent)
	case <-stream.done:
		refuse(w, http.StatusNotFound, "unknown stream", "the event stream closed; reconnect")
	case <-ctx.Done():
		refuse(w, http.StatusServiceUnavailable, "stream busy", "the event stream did not apply the update; reconnect")
	}
}
