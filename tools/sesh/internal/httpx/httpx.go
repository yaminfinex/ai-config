// Package httpx builds the bounded HTTP clients every sesh network caller
// uses. http.DefaultClient has no timeout at any layer, so a single stalled
// request (connection up, response never delivered) wedges its caller
// indefinitely — the Mac shipper wedge class. Every store round trip in this
// codebase (shipper, updater, status ping) goes through a client from here.
package httpx

import (
	"net"
	"net/http"
	"time"
)

// NewClient returns a client bounded at every layer: dial and TLS stalls
// surface within seconds, a response-header stall within 30s, and timeout is
// the hard cap on the whole exchange (size it so the largest expected body
// still fits over a slow relayed link). maxIdlePerHost keeps that many warm
// connections to the single store host; callers issuing bounded-parallel
// requests pass their concurrency bound so workers reuse connections instead
// of churning through the default of 2.
func NewClient(timeout time.Duration, maxIdlePerHost int) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
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
		},
	}
}
