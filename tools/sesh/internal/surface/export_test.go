package surface

// Test-only seams for the projection single-flight/serve-stale gates: the
// staged hook makes "a rebuild is in flight right now" (and "this rebuild
// fails") provable states instead of timing guesses, and the idle wait makes
// convergence deterministic.

// RebuildStage re-exports the rebuild hook stages for the external test
// package.
type RebuildStage = rebuildStage

const (
	RebuildStart   = rebuildStart
	RebuildStamped = rebuildStamped
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

// TranscriptWindowMessages re-exports the transcript window bound so the
// external tests assert against the one constant instead of a copied magic
// number.
const TranscriptWindowMessages = transcriptWindowMessages
