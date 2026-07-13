package ship

// Regression coverage for the wedged-shipper failure mode: a store that
// accepts the TCP connection but never answers must degrade into the
// unreachable-store reaction (hold position, jittered backoff — wire doc,
// Error Catalog, store_unavailable), not park the daemon inside one round
// trip forever before its first log line.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The nil-HTTPClient fallback must never regress to the timeout-less
// http.DefaultClient: with no bound on a round trip, the hold-position state
// machine is unreachable when a response simply never arrives.
func TestClientFallbackBoundsRoundTrips(t *testing.T) {
	hc := (&Client{}).httpClient()
	if hc == http.DefaultClient {
		t.Fatal("nil-HTTPClient fallback is http.DefaultClient, which never times out")
	}
	if hc.Timeout <= 0 {
		t.Fatal("fallback client has no overall round-trip timeout")
	}
	tr, ok := hc.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("fallback transport is %T, want *http.Transport", hc.Transport)
	}
	if tr.ResponseHeaderTimeout <= 0 {
		t.Fatal("fallback transport has no response-header timeout")
	}
}

// A stalled store (request delivered, response withheld) must surface from
// RunOnce as a hold within the fallback client's bound, on the same
// recovery-GET path a fresh registry takes at daemon start. HTTPClient stays
// nil here so the round trip runs through the production fallback — the
// package fallback's own round-trip bound is what fires, shortened in place
// for the test and restored afterwards.
func TestStalledStoreSurfacesAsHold(t *testing.T) {
	h := newHarness(t)
	h.writeClaude("proj", uuidNormal, fixture(t, "claude-normal.jsonl"))

	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // withhold the response until the client gives up
	}))
	t.Cleanup(stalled.Close)

	fallback := (&Client{}).httpClient()
	if fallback == http.DefaultClient {
		t.Fatal("nil-HTTPClient fallback is http.DefaultClient; a stalled store would park RunOnce forever")
	}
	prevTimeout := fallback.Timeout
	fallback.Timeout = 250 * time.Millisecond
	t.Cleanup(func() { fallback.Timeout = prevTimeout })

	h.shipper.Client = &Client{
		BaseURL:  stalled.URL,
		Hostname: "testhost",
		OSUser:   "testuser",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.shipper.RunOnce(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, errHold) {
			t.Fatalf("RunOnce against a stalled store: got %v, want errHold", err)
		}
	case <-time.After(5 * time.Second):
		cancel() // release the withheld handler so the server can close
		t.Fatal("RunOnce still blocked after 5s; the fallback round-trip bound did not fire")
	}
}
