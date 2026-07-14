package httpx

// The bulk client's contract, both halves: a transfer that keeps moving
// bytes — however slowly — is never killed by wall clock, and a
// zero-progress stall is cut at the idle bound. A wall-clock cap here is the
// wedge class reborn as a livelock: the caller retries the killed transfer
// at the same offset and it dies the same way forever.

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// drippingReader yields one byte per interval: a request body that is always
// progressing and always slower than the test's idle bound in total.
type drippingReader struct {
	remaining int
	interval  time.Duration
}

func (d *drippingReader) Read(p []byte) (int, error) {
	if d.remaining == 0 {
		return 0, io.EOF
	}
	time.Sleep(d.interval)
	d.remaining--
	p[0] = 'x'
	return 1, nil
}

func TestBulkClientSurvivesSlowProgressingResponse(t *testing.T) {
	const total = 40
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		for range total {
			time.Sleep(20 * time.Millisecond)
			if _, err := w.Write([]byte("y")); err != nil {
				return
			}
			f.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	// Whole response takes ~800ms; idle bound is 200ms. Each byte re-arms
	// the watchdog, so the transfer must survive despite total >> idle.
	client := NewBulkClient(200*time.Millisecond, 2)
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("slow progressing response killed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading slow progressing response: %v", err)
	}
	if len(body) != total {
		t.Fatalf("read %d bytes, want %d", len(body), total)
	}
}

func TestBulkClientSurvivesSlowUploadAndACK(t *testing.T) {
	const total = 30
	var got bytes.Buffer
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(&got, r.Body); err != nil {
			t.Errorf("server reading slow upload: %v", err)
		}
		// The ACK lands after the whole slow upload; ResponseHeaderTimeout
		// (not the watchdog) covers this gap in production.
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// Upload takes ~600ms at one byte per 20ms; idle bound is 200ms. Every
	// request-body read by the transport re-arms the watchdog.
	client := NewBulkClient(200*time.Millisecond, 2)
	req, err := http.NewRequest(http.MethodPut, srv.URL, &drippingReader{remaining: total, interval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("slow progressing upload killed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || got.Len() != total {
		t.Fatalf("upload status %d, server got %d bytes; want 200 and %d", resp.StatusCode, got.Len(), total)
	}
}

func TestBulkClientCutsZeroProgressStall(t *testing.T) {
	stall := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		if _, err := w.Write([]byte("partial")); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		<-stall // headers and a few bytes delivered, then nothing, forever
	}))
	t.Cleanup(func() { close(stall); srv.Close() })

	client := NewBulkClient(150*time.Millisecond, 2)
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET before the stall: %v", err)
	}
	defer resp.Body.Close()
	start := time.Now()
	_, err = io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("zero-progress mid-body stall was not cut")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("stall cut took %v, want around the 150ms idle bound", elapsed)
	}
	if !strings.Contains(err.Error(), "zero-progress") && !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("stall error %q does not surface the watchdog cancellation", err)
	}
}
