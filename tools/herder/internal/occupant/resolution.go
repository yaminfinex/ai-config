package occupant

import "ai-config/tools/herder/internal/registry/v2"

// Resolution joins one live occupant observation to the registry without
// consulting any stored seat coordinate. Matches are proved exclusively by
// Verdict's transcript SID/recorded-lineage membership rule.
type Resolution struct {
	Observation Observation
	Outcome     Outcome
	Row         *v2.SessionRecord
	Matches     int
}

// Resolve matches an observation against every registry session. claimedGUID
// is an optional provenance fence (HERDER_GUID or a seat credential): when it
// names a different row from the transcript match, the result is a positive
// mismatch rather than a choice between the two identities.
func Resolve(obs Observation, rows []v2.SessionRecord, claimedGUID string) Resolution {
	result := Resolution{Observation: obs, Outcome: outcomeForObservation(obs)}
	if obs.Status != Occupied || obs.SID == "" {
		return result
	}

	var matches []v2.SessionRecord
	for i := range rows {
		if rows[i].Tool != "" && obs.Tool != "" && rows[i].Tool != obs.Tool {
			continue
		}
		outcome := Verdict(obs, rows[i])
		if outcome.Status == Match {
			matches = append(matches, rows[i])
		}
	}
	result.Matches = len(matches)
	if len(matches) > 1 {
		result.Outcome = Outcome{Status: OutcomeAmbiguous, SID: obs.SID}
		return result
	}
	if len(matches) == 0 {
		result.Outcome = Outcome{Status: PositiveMismatch, SID: obs.SID}
		return result
	}
	if claimedGUID != "" && matches[0].GUID != claimedGUID {
		result.Outcome = Outcome{Status: PositiveMismatch, SID: obs.SID}
		return result
	}

	matched := matches[0]
	result.Row = &matched
	result.Outcome = Verdict(obs, matched)
	return result
}

func outcomeForObservation(obs Observation) Outcome {
	switch obs.Status {
	case Vacant:
		return Outcome{Status: NoOccupant, Reason: ReasonVacant}
	case PaneGone:
		return Outcome{Status: NoOccupant, Reason: ReasonPaneGone}
	case Unprobeable:
		return Outcome{Status: OutcomeUnprobeable}
	default:
		return Outcome{Status: OutcomeAmbiguous}
	}
}

// ExactEvidence reports whether an observation has an exact transcript join.
// Cohort-only Claude evidence can prove a match for a claimed row, but its
// positive mismatch is non-authoritative: callers must refuse without naming
// an owner or performing turnover/displacement writes.
func ExactEvidence(obs Observation) bool {
	for _, signal := range obs.Evidence {
		if signal == SignalFD || signal == SignalAgentSession {
			return true
		}
	}
	return false
}
