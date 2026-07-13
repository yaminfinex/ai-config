package surface

// Test-only seams for the projection single-flight/serve-stale gate: the
// barrier makes "a rebuild is in flight right now" a provable state instead
// of a timing guess, and the idle wait makes convergence deterministic.

// SetRebuildBarrier installs fn to run at the start of every projection
// rebuild. Install it before serving requests; it is captured per rebuild
// under the projection lock.
func (s *SQLStore) SetRebuildBarrier(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rebuildBarrier = fn
}

// WaitProjectionIdle blocks until no projection rebuild is in flight.
func (s *SQLStore) WaitProjectionIdle() {
	for {
		s.mu.Lock()
		run := s.refresh
		s.mu.Unlock()
		if run == nil {
			return
		}
		<-run.done
	}
}
