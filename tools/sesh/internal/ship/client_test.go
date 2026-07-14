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

	"sesh/internal/httpx"
)

// The nil-HTTPClient fallback must never regress to the timeout-less
// http.DefaultClient: with no bound on a round trip, the hold-position state
// machine is unreachable when a response simply never arrives. The bound
// must be progress-sensitive, though — a wall-clock cap kills a legitimately
// slow full-size PUT that is still moving bytes, and the retry then dies the
// same way at the same offset forever (the wedge reborn as a livelock).
func TestClientFallbackBoundsRoundTrips(t *testing.T) {
	hc := (&Client{}).httpClient()
	if hc == http.DefaultClient {
		t.Fatal("nil-HTTPClient fallback is http.DefaultClient, which never times out")
	}
	if hc.Timeout != 0 {
		t.Fatalf("fallback client carries a wall-clock cap (%v); a slow progressing PUT would be killed and retried at the same offset forever", hc.Timeout)
	}
	wd, ok := hc.Transport.(*httpx.IdleWatchdogTransport)
	if !ok {
		t.Fatalf("fallback transport is %T, want the idle-progress watchdog", hc.Transport)
	}
	if wd.Idle <= 0 {
		t.Fatal("watchdog transport has no idle bound; a zero-progress mid-body stall would hang forever")
	}
	tr, ok := wd.Base.(*http.Transport)
	if !ok {
		t.Fatalf("watchdog base transport is %T, want *http.Transport", wd.Base)
	}
	if tr.ResponseHeaderTimeout <= 0 {
		t.Fatal("fallback transport has no response-header timeout")
	}
}

// A stalled store (request delivered, response withheld — zero bytes moving)
// must surface from RunOnce as a hold within the fallback client's idle
// bound, on the same recovery-GET path a fresh registry takes at daemon
// start. HTTPClient stays nil here so the round trip runs through the
// production fallback; the package fallback is swapped for a short-idle
// twin of itself for the test and restored afterwards.
func TestStalledStoreSurfacesAsHold(t *testing.T) {
	h := newHarness(t)
	h.writeClaude("proj", uuidNormal, fixture(t, "claude-normal.jsonl"))

	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // withhold the response until the client gives up
	}))
	t.Cleanup(stalled.Close)

	if (&Client{}).httpClient() == http.DefaultClient {
		t.Fatal("nil-HTTPClient fallback is http.DefaultClient; a stalled store would park RunOnce forever")
	}
	prev := defaultHTTPClient
	defaultHTTPClient = httpx.NewBulkClient(250*time.Millisecond, 1)
	t.Cleanup(func() { defaultHTTPClient = prev })

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
