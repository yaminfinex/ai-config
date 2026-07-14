package store

// Version census: the store records each shipper's self-reported
// version from the wire User-Agent into last_seen bookkeeping at PUT time.
// Informational only — a client that sends no UA, a pre-census Go default
// UA, or garbage ships exactly as before and is recorded as unknown.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sesh/internal/wire"
)

// putReqUA is putReq with an explicit User-Agent on the request.
func putReqUA(t *testing.T, st *Store, ua string, offset int64, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/files/claude/"+testSession+"/"+testFile+"/bytes?offset="+itoa(offset), bytes.NewReader(body))
	req.Header.Set("Content-Type", wire.ContentTypeBytes)
	req.Header.Set(wire.HeaderWireVersion, "1")
	req.Header.Set(wire.HeaderHostname, "node-a")
	req.Header.Set(wire.HeaderOSUser, "grace")
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	return rr
}

func lastSeenVersion(t *testing.T, st *Store) *string {
	t.Helper()
	var v sql.NullString
	err := st.db.QueryRow(`SELECT shipper_version FROM last_seen WHERE hostname = 'node-a' AND os_user = 'grace'`).Scan(&v)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Valid {
		return nil
	}
	return &v.String
}

func TestPUTRecordsShipperVersionFromUserAgent(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	body := []byte("line one\n")

	// A census-aware shipper: the tag-format version is recorded verbatim.
	if rr := putReqUA(t, st, "sesh-ship/sesh-v0.1.9", 0, body); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d body %s", rr.Code, rr.Body)
	}
	if v := lastSeenVersion(t, st); v == nil || *v != "sesh-v0.1.9" {
		t.Fatalf("shipper_version = %v, want sesh-v0.1.9", v)
	}

	// A pre-census client (Go default UA) still ships and overwrites the
	// version with unknown: the census reports what the node runs NOW.
	if rr := putReqUA(t, st, "Go-http-client/1.1", int64(len(body)), []byte("line two\n")); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d body %s", rr.Code, rr.Body)
	}
	if v := lastSeenVersion(t, st); v != nil {
		t.Fatalf("shipper_version = %q, want NULL after non-sesh UA", *v)
	}

	// Upgrade visible again on the next identifying PUT.
	if rr := putReqUA(t, st, "sesh-ship/sesh-v0.2.0", 2*int64(len(body)), []byte("line 3\n")); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d body %s", rr.Code, rr.Body)
	}
	if v := lastSeenVersion(t, st); v == nil || *v != "sesh-v0.2.0" {
		t.Fatalf("shipper_version = %v, want sesh-v0.2.0", v)
	}
}

func TestPUTToleratesAbsentAndMalformedUserAgent(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	for _, ua := range []string{"", "sesh-ship/", "sesh-ship/" + string(bytes.Repeat([]byte("9"), 65)), "sesh-ship/0.1.9<script>", "curl/8.5.0"} {
		if rr := putReqUA(t, st, ua, 0, []byte("hello\n")); rr.Code != http.StatusOK {
			t.Fatalf("UA %q: PUT = %d body %s (absence/garbage must never block shipping)", ua, rr.Code, rr.Body)
		}
		if v := lastSeenVersion(t, st); v != nil {
			t.Fatalf("UA %q: shipper_version = %q, want NULL", ua, *v)
		}
	}
	// A trailing product token after the version is tolerated (RFC UA form).
	if rr := putReqUA(t, st, "sesh-ship/0.1.9 (linux; amd64)", int64(len("hello\n")), []byte("more\n")); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d body %s", rr.Code, rr.Body)
	}
	if v := lastSeenVersion(t, st); v == nil || *v != "0.1.9" {
		t.Fatalf("shipper_version = %v, want 0.1.9", v)
	}
}

// TestShipperVersionColumnSurvivesReopen covers the additive-DDL guard: a
// store restarted over an existing database must not fail initSchema, and
// bookkeeping written before the restart must remain readable.
func TestShipperVersionColumnSurvivesReopen(t *testing.T) {
	logBuf := new(bytes.Buffer)
	st := newTestStore(t, logBuf)
	if rr := putReqUA(t, st, "sesh-ship/sesh-v0.1.9", 0, []byte("hello\n")); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d body %s", rr.Code, rr.Body)
	}
	dir := st.dir
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(t.Context(), Config{Dir: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() {
		_ = st2.Close()
	})
	if v := lastSeenVersion(t, st2); v == nil || *v != "sesh-v0.1.9" {
		t.Fatalf("shipper_version after reopen = %v, want sesh-v0.1.9", v)
	}
}

func TestShipperVersionFromUA(t *testing.T) {
	cases := map[string]string{
		"sesh-ship/sesh-v0.1.9":          "sesh-v0.1.9",
		"sesh-ship/0.1.9":                "0.1.9",
		"sesh-ship/sesh-v0.1.9-3-gabc12": "sesh-v0.1.9-3-gabc12",
		"sesh-ship/dev":                  "dev",
		"sesh-ship/0.1.9 extra/1.0":      "0.1.9",
		"sesh-ship/":                     "",
		"sesh-ship/0.1.9\x00":            "",
		"Go-http-client/1.1":             "",
		"":                               "",
	}
	for ua, want := range cases {
		if got := shipperVersionFromUA(ua); got != want {
			t.Errorf("shipperVersionFromUA(%q) = %q, want %q", ua, got, want)
		}
	}
}

// The machine-readable /v1/nodes endpoint carries the census as an additive
// omitempty field: present for nodes that self-reported, absent (old JSON
// shape) for pre-UA/NULL rows. The hostile-string case guards the JSON
// encoder on the direct-insert path — capture-time bounding/allowlisting
// means such a value cannot arrive over the wire, but a row is durable and
// the encoder must stay safe regardless of how it got there.
func TestNodesEndpointCarriesShipperVersion(t *testing.T) {
	st := newTestStore(t, new(bytes.Buffer))
	if rr := putReqUA(t, st, "sesh-ship/sesh-v0.1.9", 0, []byte("hello\n")); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d body %s", rr.Code, rr.Body)
	}
	now := "2026-07-14T00:00:00Z"
	if _, err := st.db.Exec(`INSERT INTO last_seen(hostname, os_user, last_put_at) VALUES ('pre-census-node', 'bob', ?)`, now); err != nil {
		t.Fatal(err)
	}
	hostile := `"</script><img src=x>&` + " "
	if _, err := st.db.Exec(`INSERT INTO last_seen(hostname, os_user, last_put_at, shipper_version) VALUES ('hostile-node', 'mallory', ?, ?)`, now, hostile); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/nodes", nil)
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /v1/nodes = %d body %s", rr.Code, rr.Body)
	}
	var resp struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v\n%s", err, rr.Body)
	}
	byHost := map[string]map[string]any{}
	for _, n := range resp.Nodes {
		byHost[n["hostname"].(string)] = n
	}
	if v, ok := byHost["node-a"]["shipper_version"]; !ok || v != "sesh-v0.1.9" {
		t.Fatalf("populated row: shipper_version = %v (present=%v), want sesh-v0.1.9", v, ok)
	}
	if _, ok := byHost["pre-census-node"]["shipper_version"]; ok {
		t.Fatalf("pre-census row must keep the old JSON shape (no shipper_version key): %v", byHost["pre-census-node"])
	}
	if v := byHost["hostile-node"]["shipper_version"]; v != hostile {
		t.Fatalf("hostile value must round-trip exactly through the encoder: %q != %q", v, hostile)
	}
	// encoding/json HTML-escapes <, >, & (and U+2028/29), so a hostile row
	// cannot smuggle markup into any consumer that sniffs the raw body.
	if bytes.Contains(rr.Body.Bytes(), []byte("</script>")) || bytes.Contains(rr.Body.Bytes(), []byte("<img")) {
		t.Fatalf("raw /v1/nodes body carries unescaped markup:\n%s", rr.Body)
	}
}
