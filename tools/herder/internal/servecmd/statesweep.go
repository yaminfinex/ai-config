package servecmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/webstate"
)

const (
	// StateSweepCadence is how often the serve sweeps browser state after
	// the sweep it runs at start.
	StateSweepCadence = time.Hour
	// AbsentAgentRetention is how long an agent must have been gone from
	// the roster and the store's history before its agent-keyed rows go.
	AbsentAgentRetention = 7 * 24 * time.Hour
	// TombstoneRetention is how long an agent-keyed namespace keeps a delete.
	TombstoneRetention = 30 * 24 * time.Hour
)

// agentKeyedNamespaces are keyed by agent name and mean nothing once the
// agent is gone; they are the only namespaces the sweep touches. notes
// (note ids), spaces and spaces.members (space ids) are owner content: the
// sweep never removes their rows or purges their tombstones, since clients
// replay their caches additively and a purged tombstone would let a stale
// browser bring a deleted note or space back. Inside read.markers a stale
// client re-adding a swept row is acceptable: last-write-wins lets any newer
// write beat it, and the next sweep removes it again.
var agentKeyedNamespaces = map[string]bool{"read.markers": true}

type stateSweeper interface {
	Sweep(webstate.SweepPolicy) ([]webstate.SweepResult, error)
}

// stateSweepPolicy decides at now. An agent-keyed row goes when its agent is
// not on the roster, the store's history last saw it (or closed it) longer
// than the retention ago or never, and the row itself was last written
// longer than the retention ago. A name reused later simply starts fresh.
func stateSweepPolicy(now time.Time, roster []hcomidentity.Row, projection *agentstore.Projection) webstate.SweepPolicy {
	present := map[string]bool{}
	for _, row := range roster {
		present[row.Name] = true
		if row.BaseName != "" {
			present[row.BaseName] = true
		}
	}
	cutoff := now.Add(-AbsentAgentRetention)
	return webstate.SweepPolicy{
		Namespaces:       agentKeyedNamespaces,
		TombstonesBefore: now.Add(-TombstoneRetention).UnixMilli(),
		Absent: func(namespace string, row webstate.Row) bool {
			if !agentKeyedNamespaces[namespace] || present[row.Key] || row.Updated >= cutoff.UnixMilli() {
				return false
			}
			if projection != nil {
				if latest := projection.Latest(row.Key); latest != nil {
					seen := latest.LastSeen
					if latest.Closed != nil && latest.Closed.After(seen) {
						seen = *latest.Closed
					}
					if !seen.Before(cutoff) {
						return false
					}
				}
			}
			return true
		},
	}
}

// sweepStateOnce sweeps with a fresh roster and the projection of the
// latest successful fold; without either it skips the absence rule (an
// unreadable roster, or a store read that failed after an earlier one
// succeeded, is not evidence of absence) and still purges old tombstones
// in agent-keyed namespaces.
func sweepStateOnce(deps dependencies, store stateSweeper, publish func(stateChange), stderr io.Writer) {
	now := time.Now()
	if deps.now != nil {
		now = deps.now()
	}
	var projection *agentstore.Projection
	if deps.projection != nil {
		projection = deps.projection.current()
	}
	var roster []hcomidentity.Row
	var rosterErr error = fmt.Errorf("no roster reader")
	if deps.roster != nil {
		roster, rosterErr = deps.roster()
	}
	policy := stateSweepPolicy(now, roster, projection)
	if rosterErr != nil || projection == nil {
		policy.Absent = nil
	}
	results, err := store.Sweep(policy)
	if err != nil && stderr != nil {
		fmt.Fprintf(stderr, "herder serve: state sweep: %v\n", err)
	}
	for _, result := range results {
		publish(stateChange{Namespace: result.Namespace, Rev: result.Rev})
	}
}

// startStateSweep runs the sweep once now and then every cadence, off any
// request path.
func startStateSweep(ctx context.Context, deps dependencies, store stateSweeper, cadence time.Duration, stderr io.Writer) {
	publish := func(change stateChange) {
		if deps.stateChanges != nil {
			deps.stateChanges.publish(change)
		}
	}
	go func() {
		sweepStateOnce(deps, store, publish, stderr)
		ticker := time.NewTicker(cadence)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweepStateOnce(deps, store, publish, stderr)
			}
		}
	}()
}
