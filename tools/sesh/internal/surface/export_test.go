package surface

import (
	"context"
	"html/template"

	"sesh/internal/wire"
)

// Test-only seams for the projection single-flight/serve-stale gates: the
// staged hook makes "a rebuild is in flight right now" (and "this rebuild
// fails") provable states instead of timing guesses, and the idle wait makes
// convergence deterministic.

// RebuildStage re-exports the rebuild hook stages for the external test
// package.
type RebuildStage = rebuildStage

const (
	RebuildStart      = rebuildStart
	RebuildStamped    = rebuildStamped
	RebuildNodeSlices = rebuildNodeSlices
)

// SetRebuildHook installs fn to run at each stage of every projection
// rebuild; a returned error aborts the rebuild exactly like the query at
// that stage failing. Install it before serving requests; it is captured
// per rebuild under the projection lock.
func (s *SQLStore) SetRebuildHook(fn func(RebuildStage) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rebuildHook = fn
}

// WaitProjectionIdle blocks until no projection rebuild is in flight.
func (s *SQLStore) WaitProjectionIdle() {
	s.waitProjectionIdle()
}

// SetRecencyTemplateForTest swaps the recency page template — the
// log-contract gate's seam for driving the shared render-failure logging
// path with a deliberately failing, identifier-carrying template.
func (s *Server) SetRecencyTemplateForTest(t *template.Template) { s.recencyTmpl = t }

// TranscriptWindowMessages re-exports the transcript window bound so the
// external tests assert against the one constant instead of a copied magic
// number.
const TranscriptWindowMessages = transcriptWindowMessages

// ClearGlobalRankingForTest empties the all-nodes ranked list while leaving
// the per-node slices, built flag, and stamp in place. The provenance half
// of the filter gate uses it: filtered paging must read the PREBUILT
// per-node slice, never derive pages from the global ranking at request
// time.
func (s *SQLStore) ClearGlobalRankingForTest() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ranking = nil
}

// RankedInspected reads the request-path work counter: ranked entries
// examined during selection and paging since the store opened. The
// work-scaling gate asserts per-request deltas against the target slice or
// page size.
func (s *SQLStore) RankedInspected() int64 {
	return s.rankedInspected.Load()
}

// HydrateLiveForTest runs the live single-session hydration shape over a
// LIST of keys — the deliberately regressed page-hydration path, whose
// per-key queries walk every index row of every listed session. The
// max-size-sessions gate's negative self-check proves its no-row-walks
// detector actually flags this shape's SQL; without that proof the
// detector is assumed, not tested.
func (s *SQLStore) HydrateLiveForTest(ctx context.Context, tool wire.Tool, logicalIDs []string) (int, error) {
	keys := make([]sessionKey, len(logicalIDs))
	for i, id := range logicalIDs {
		keys[i] = sessionKey{tool, id}
	}
	sums, err := s.hydrateSessions(ctx, keys)
	return len(sums), err
}

// WalkFilteredForTest is the deliberately regressed selection shape — the
// per-request corpus walk over the global ranking — wired through the same
// inspection seam the real path charges. The work-scaling gate's negative
// self-check proves the seam plus bound actually flags this shape; without
// that proof the detector is assumed, not tested.
func (s *SQLStore) WalkFilteredForTest(hostname, osUser string) int {
	s.mu.Lock()
	ranking := s.ranking
	s.mu.Unlock()
	s.inspectRanked(len(ranking))
	n := 0
	for _, r := range ranking {
		if r.hostname == hostname && r.osUser == osUser {
			n++
		}
	}
	return n
}
