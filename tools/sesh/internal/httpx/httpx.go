// Package httpx builds the bounded HTTP clients every sesh network caller
// uses. http.DefaultClient has no timeout at any layer, so a single stalled
// request (connection up, response never delivered) wedges its caller
// indefinitely — the Mac shipper wedge class. Every store round trip in this
// codebase goes through a client from here: bulk transfers (shipper PUTs and
// recovery GETs, updater downloads) through NewBulkClient, small interactive
// calls (the status ping) through NewClient.
package httpx

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// NewClient returns a client with a hard wall-clock cap on the whole
// exchange. The cap is NOT progress-sensitive, so this constructor is only
// for small interactive calls (a health ping, a version check) where the
// response is tiny and the caller wants to fail within seconds. Bulk
// transfers must use NewBulkClient: a wall-clock cap kills a legitimately
// slow multi-MB transfer that is still making progress, and the caller's
// retry then dies the same way at the same offset — a time-based livelock,
// the old wedge class at a different boundary. maxIdlePerHost keeps that
// many warm connections to the single store host.
func NewClient(timeout time.Duration, maxIdlePerHost int) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: newTransport(maxIdlePerHost),
	}
}

// NewBulkClient returns a client for multi-MB transfers over links of
// unknown quality: no overall deadline — a transfer that keeps moving bytes
// is never killed by wall clock — while every zero-progress failure mode
// stays bounded. Dial, TLS, and response-header stalls surface via the
// transport bounds; a mid-body stall (either direction: request upload or
// response download) is cut by an idle watchdog that cancels the request
// when no byte has moved for idle. Callers issuing bounded-parallel requests
// pass their concurrency bound as maxIdlePerHost so workers reuse
// connections instead of churning through the default of 2.
func NewBulkClient(idle time.Duration, maxIdlePerHost int) *http.Client {
	return &http.Client{
		Transport: &IdleWatchdogTransport{Base: newTransport(maxIdlePerHost), Idle: idle},
	}
}

func newTransport(maxIdlePerHost int) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 15 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   maxIdlePerHost,
	}
}

// IdleWatchdogTransport arms one timer per request; any byte moved in either
// direction re-arms it, and expiry cancels the request's context. The
// request body is read by the transport during upload and the response body
// by the caller during download, so wrapping both covers the whole exchange;
// the gap between upload end and first header byte is covered by
// ResponseHeaderTimeout. Exported (module-internal) so callers' regression
// tests can assert their client really is watchdog-bounded.
type IdleWatchdogTransport struct {
	Base http.RoundTripper
	Idle time.Duration
}

func (t *IdleWatchdogTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	timer := time.AfterFunc(t.Idle, func() {
		cancel(fmt.Errorf("zero-progress stall: no byte moved for %v", t.Idle))
	})
	req = req.WithContext(ctx)
	if req.Body != nil {
		req.Body = &watchdogBody{rc: req.Body, timer: timer, idle: t.Idle}
	}
	resp, err := t.Base.RoundTrip(req)
	if err != nil {
		timer.Stop()
		cancel(nil)
		if cause := context.Cause(ctx); cause != nil && ctx.Err() != nil {
			return nil, fmt.Errorf("%w (%v)", err, cause)
		}
		return nil, err
	}
	// Once the caller closes the (fully read) response body the request is
	// finished and the late cancel is inert; stopping the timer there is
	// hygiene, not correctness.
	resp.Body = &watchdogBody{rc: resp.Body, timer: timer, idle: t.Idle, done: func() {
		timer.Stop()
		cancel(nil)
	}}
	return resp, nil
}

type watchdogBody struct {
	rc    io.ReadCloser
	timer *time.Timer
	idle  time.Duration
	done  func()
}

func (b *watchdogBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		b.timer.Reset(b.idle)
	}
	return n, err
}

func (b *watchdogBody) Close() error {
	if b.done != nil {
		b.done()
	}
	return b.rc.Close()
}
