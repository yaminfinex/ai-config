package registry

import (
	"sort"
	"time"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

type LiveEvidenceState string

const (
	LiveEvidenceAbsent      LiveEvidenceState = "absent"
	LiveEvidenceUnavailable LiveEvidenceState = "unavailable"
)

type BindingSelectionStatus string

const (
	BindingSelected BindingSelectionStatus = "selected"
	BindingMissing  BindingSelectionStatus = "missing"
	BindingDeferred BindingSelectionStatus = "deferred"
)

// LatestSufficientBinding adjudicates history only after the live source was
// successfully consulted and returned no match. An outage never arms history.
func LatestSufficientBinding(rec v2.SessionRecord, field string, live LiveEvidenceState) (v2.BindingFact, BindingSelectionStatus) {
	if live != LiveEvidenceAbsent {
		return v2.BindingFact{}, BindingDeferred
	}
	tombstoned := make(map[string]bool, len(rec.BindingTombstones))
	for _, marker := range rec.BindingTombstones {
		tombstoned[marker.BindingID] = true
	}
	candidates := make([]v2.BindingFact, 0, len(rec.Bindings))
	for _, fact := range rec.Bindings {
		if fact.Field != field || tombstoned[fact.ID] || evidenceRank(fact.EvidenceClass) < evidenceRank(v2.EvidenceAttested) {
			continue
		}
		candidates = append(candidates, fact)
	}
	if len(candidates) == 0 {
		return v2.BindingFact{}, BindingMissing
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		ri, rj := evidenceRank(candidates[i].EvidenceClass), evidenceRank(candidates[j].EvidenceClass)
		if ri != rj {
			return ri > rj
		}
		return candidates[i].ObservedAt > candidates[j].ObservedAt
	})
	return candidates[0], BindingSelected
}

func evidenceRank(class string) int {
	switch class {
	case v2.EvidenceLiveVerified:
		return 4
	case v2.EvidenceAttested:
		return 3
	case v2.EvidenceHarvest, v2.EvidenceCarried:
		return 2
	case v2.EvidenceAssumed:
		return 1
	default:
		return 0
	}
}

// AttestationRateLimit returns the duration remaining in the per-guid rolling
// window. Only committed attestations appear in the row, so failed proof
// attempts intentionally do not consume the window.
func AttestationRateLimit(rec v2.SessionRecord, now time.Time, window time.Duration) (time.Duration, bool) {
	var latest time.Time
	for _, attestation := range rec.Attestations {
		observed, err := time.Parse(time.RFC3339, attestation.ObservedAt)
		if err == nil && observed.After(latest) {
			latest = observed
		}
	}
	if latest.IsZero() {
		return 0, false
	}
	remaining := latest.Add(window).Sub(now)
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}
