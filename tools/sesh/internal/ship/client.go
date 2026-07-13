package ship

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"sesh/internal/wire"
)

// Client speaks the frozen wire contract to the store. A transport-level
// failure (store unreachable) is returned as err and treated exactly like
// store_unavailable: hold position, jittered backoff, no local queue.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Hostname   string
	OSUser     string
}

// defaultHTTPClient bounds every store round trip. http.DefaultClient has no
// timeout at any layer, so a single stalled request (connection up, response
// never delivered) parks the whole pass inside one round trip and the
// unreachable-store reaction — hold position, jittered backoff (wire doc,
// Error Catalog, store_unavailable) — never gets to run. Dial and
// response-header stalls surface within seconds; the overall Timeout is the
// hard cap sized so a full-size PUT body (wire.MaxPUTBody) still fits over a
// slow relayed link.
var defaultHTTPClient = &http.Client{
	Timeout: 5 * time.Minute,
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
	},
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return defaultHTTPClient
}

// PutBytes ships one raw byte range. fingerprint is empty while the source
// file is below the fingerprint window. owner carries an observed
// SESSION_OWNER ("" = omit the header; omission never retracts). Exactly one
// of ack/werr is non-nil on a nil err.
func (c *Client) PutBytes(ctx context.Context, id Identity, offset int64, body []byte, fingerprint, owner string) (*wire.Ack, *wire.ErrorResponse, error) {
	u := c.BaseURL + fmt.Sprintf(wire.PathPUTBytesFmt,
		url.PathEscape(string(id.Tool)), url.PathEscape(id.SessionID), url.PathEscape(id.FileUUID)) +
		"?offset=" + strconv.FormatInt(offset, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, strconv.Itoa(wire.Version))
	req.Header.Set(wire.HeaderHostname, c.Hostname)
	req.Header.Set(wire.HeaderOSUser, c.OSUser)
	if fingerprint != "" {
		req.Header.Set(wire.HeaderFingerprintAlgorithm, wire.FingerprintAlgorithm)
		req.Header.Set(wire.HeaderFingerprint, fingerprint)
	}
	if owner != "" {
		req.Header.Set(wire.HeaderSessionOwner, owner)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var ack wire.Ack
		if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
			return nil, nil, fmt.Errorf("malformed ACK body: %w", err)
		}
		return &ack, nil, nil
	}
	werr, err := decodeError(resp)
	return nil, werr, err
}

// Recover asks the store what it holds for a file identity (registry
// missing/unreadable path). A wire.ErrNotFound response is returned as a
// typed werr, not an err: it means start from offset 0.
func (c *Client) Recover(ctx context.Context, id Identity) (*wire.RecoveryResponse, *wire.ErrorResponse, error) {
	u := c.BaseURL + fmt.Sprintf(wire.PathRecoveryFmt,
		url.PathEscape(string(id.Tool)), url.PathEscape(id.SessionID), url.PathEscape(id.FileUUID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set(wire.HeaderWireVersion, strconv.Itoa(wire.Version))
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var rec wire.RecoveryResponse
		if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
			return nil, nil, fmt.Errorf("malformed recovery body: %w", err)
		}
		return &rec, nil, nil
	}
	werr, err := decodeError(resp)
	return nil, werr, err
}

func decodeError(resp *http.Response) (*wire.ErrorResponse, error) {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var werr wire.ErrorResponse
	if err := json.Unmarshal(raw, &werr); err != nil || werr.Code == "" {
		return nil, fmt.Errorf("store returned HTTP %d with non-catalog body %q", resp.StatusCode, truncate(raw, 200))
	}
	return &werr, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
