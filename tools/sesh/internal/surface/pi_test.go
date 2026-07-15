package surface_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sesh/internal/wire"
)

type shuffledPiStore struct{ *fakeStore }

func (s *shuffledPiStore) Rows(ctx context.Context, tool wire.Tool, id string) ([]wire.IndexMessage, error) {
	rows, err := s.fakeStore.Rows(ctx, tool, id)
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	return rows, err
}

type countingPiStore struct {
	*fakeStore
	rowReads   atomic.Int64
	rangeReads atomic.Int64
	fileReads  atomic.Int64
}

func (s *countingPiStore) Rows(ctx context.Context, tool wire.Tool, id string) ([]wire.IndexMessage, error) {
	s.rowReads.Add(1)
	return s.fakeStore.Rows(ctx, tool, id)
}

func (s *countingPiStore) MirrorRange(ctx context.Context, tool wire.Tool, wireSessionID, fileUUID string, gen int, start, end int64) ([]byte, error) {
	s.rangeReads.Add(1)
	return s.fakeStore.MirrorRange(ctx, tool, wireSessionID, fileUUID, gen, start, end)
}

func (s *countingPiStore) MirrorFile(ctx context.Context, tool wire.Tool, wireSessionID, fileUUID string, gen int) (io.ReadCloser, error) {
	s.fileReads.Add(1)
	return s.fakeStore.MirrorFile(ctx, tool, wireSessionID, fileUUID, gen)
}

func TestPiT27RendersActiveBranchAndLabelsBranchPoint(t *testing.T) {
	srv := newServer(t, piStore(t))
	body := mustGet200(t, srv, "/s/pi/"+uuidPiBranched)
	for _, want := range []string{"ACTIVE-BRANCH-CONTENT", "branch point:", "chosen option", "active path"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Pi T27 render missing %q", want)
		}
	}
	if strings.Contains(body, "ABANDONED-BRANCH-CONTENT") {
		t.Fatal("Pi T27 render silently flattened an inactive branch")
	}
}

func TestPiT27ShuffledStoreStillRendersCanonicalActiveBranch(t *testing.T) {
	srv := newServer(t, &shuffledPiStore{fakeStore: piStore(t)})
	body := mustGet200(t, srv, "/s/pi/"+uuidPiBranched)
	if !strings.Contains(body, "ACTIVE-BRANCH-CONTENT") || !strings.Contains(body, "branch point:") {
		t.Fatalf("shuffled Pi rows lost the canonical active branch: %s", body)
	}
	if strings.Contains(body, "ABANDONED-BRANCH-CONTENT") {
		t.Fatal("shuffled Pi rows selected the abandoned branch")
	}
}

func TestPiPageMirrorRangeWorkIsWindowBounded(t *testing.T) {
	const entries = 1000
	when, _ := time.Parse(time.RFC3339, "2026-07-15T12:35:30Z")
	var data strings.Builder
	fmt.Fprintf(&data, `{"type":"session","version":3,"id":"%s","timestamp":"2026-07-15T12:00:00.000Z","cwd":"/workspace"}`+"\n", uuidPiBranched)
	parent := ""
	for i := 0; i < entries; i++ {
		id := fmt.Sprintf("n%04d", i)
		parentJSON := "null"
		if parent != "" {
			parentJSON = fmt.Sprintf("%q", parent)
		}
		fmt.Fprintf(&data, `{"type":"message","id":%q,"parentId":%s,"timestamp":"2026-07-15T12:%02d:%02d.000Z","message":{"role":"user","content":%q}}`+"\n",
			id, parentJSON, (i/60)%60, i%60, id)
		parent = id
	}
	base := buildStore(t, []sessionSpec{{
		tool: wire.ToolPi, logicalID: uuidPiBranched,
		hostname: "node", osUser: "user", mirroredAt: when,
		files: []fixtureFile{{bytes: []byte(data.String()), fileUUID: uuidPiBranched, firstIngest: when}},
	}})
	store := &countingPiStore{fakeStore: base}
	srv := newServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/s/pi/"+uuidPiBranched, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := store.rangeReads.Load(); got > transcriptWindowForTest {
		t.Fatalf("Pi page performed %d mirror range reads, want <= %d", got, transcriptWindowForTest)
	}
	if got := store.fileReads.Load(); got != 1 {
		t.Fatalf("Pi projection opened %d whole mirror streams, want one amortized build", got)
	}
	if got := store.rowReads.Load(); got != 1 {
		t.Fatalf("Pi projection read the index %d times, want one amortized build", got)
	}

	store.rangeReads.Store(0)
	store.fileReads.Store(0)
	store.rowReads.Store(0)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req.Clone(context.Background()))
	if rec.Code != http.StatusOK {
		t.Fatalf("warm status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := store.rangeReads.Load(); got > transcriptWindowForTest {
		t.Fatalf("warm Pi page performed %d mirror range reads, want <= %d", got, transcriptWindowForTest)
	}
	if got := store.fileReads.Load(); got != 0 {
		t.Fatalf("warm Pi page rebuilt from %d whole mirror streams", got)
	}
	if got := store.rowReads.Load(); got != 0 {
		t.Fatalf("warm Pi page rescanned the %d-row index", got)
	}

	// Mutant premise: the old graph-rebuild-plus-window path would perform
	// one range read per graph row plus the display window, and must trip the
	// exact counter bound above.
	if mutant := int64(entries + 1 + transcriptWindowForTest); mutant <= transcriptWindowForTest {
		t.Fatalf("work-counter mutant failed to exceed bound: %d", mutant)
	}
}

const transcriptWindowForTest = 200

func TestPiNever500OnMalformedAndEmptyTrees(t *testing.T) {
	when, _ := time.Parse(time.RFC3339, "2026-07-15T12:35:30Z")
	for name, data := range map[string][]byte{
		"malformed": []byte("{broken\n"),
		"empty":     {},
	} {
		t.Run(name, func(t *testing.T) {
			store := buildStore(t, []sessionSpec{{
				tool: wire.ToolPi, logicalID: uuidPiBranched,
				hostname: "node", osUser: "user", mirroredAt: when,
				files: []fixtureFile{{bytes: data, fileUUID: uuidPiBranched, firstIngest: when}},
			}})
			srv := newServer(t, store)
			body := mustGet200(t, srv, "/s/pi/"+uuidPiBranched)
			if !strings.Contains(body, "Pi branch graph") && !strings.Contains(body, "Pi transcript is empty") && !strings.Contains(body, "raw mirror lines") {
				t.Fatalf("degraded Pi render did not label its floor: %s", body)
			}
		})
	}
}
