package send

import (
	"encoding/json"
	"fmt"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/occupant"
	"ai-config/tools/herder/internal/registry"
	"ai-config/tools/herder/internal/registry/v2"
)

// SenderIdentityRefusal is the fail-closed contract for a bus send whose
// caller cannot prove one joined hcom identity. Cause states the failed proof;
// Remedy states how the operator can repair it before retrying.
type SenderIdentityRefusal struct {
	Cause  string
	Remedy string
}

func (e *SenderIdentityRefusal) Error() string {
	return fmt.Sprintf("%s. %s", e.Cause, e.Remedy)
}

// VerifyOccupantSender proves the calling tool from its live transcript, joins
// that proof to exactly one registry session, then checks that session's
// recorded bus name is currently joined. The bus roster supplies
// addressability only; launch_context and other copied correlates do not
// participate in caller identity.
func VerifyOccupantSender(recs []registry.Record, busDir, claimedGUID string, sub occupant.Substrate) (string, error) {
	observation := occupant.SelfProbe(sub)
	resolution := occupant.Resolve(observation, v2Sessions(recs), claimedGUID)
	if resolution.Row == nil {
		return "", senderResolutionRefusal(resolution)
	}
	row := registry.Resolve(recs, resolution.Row.GUID)
	if row == nil {
		return "", &SenderIdentityRefusal{
			Cause:  "the occupant-matched registry row disappeared during sender verification",
			Remedy: "Retry from the live calling session so identity is resolved from a fresh registry snapshot",
		}
	}
	if row.HcomName == "" || row.HcomName == "null" {
		return "", &SenderIdentityRefusal{
			Cause:  "the occupant-matched registry row has no bus name",
			Remedy: "Join hcom and run `herder enroll` from this live session, then retry",
		}
	}
	rows, err := hcomidentity.List(busDir)
	if err != nil {
		return "", &SenderIdentityRefusal{
			Cause:  "the live hcom roster is unavailable: " + err.Error(),
			Remedy: "Restore access to this session's hcom bus, then retry",
		}
	}
	if _, count := hcomidentity.JoinedNamedCount(rows, row.HcomName); count != 1 {
		return "", &SenderIdentityRefusal{
			Cause:  fmt.Sprintf("occupant-matched bus name @%s resolves to %d joined rows", row.HcomName, count),
			Remedy: "Restore exactly one joined row for this live session's recorded bus name, then retry",
		}
	}
	return row.HcomName, nil
}

func senderResolutionRefusal(res occupant.Resolution) error {
	const recovery = "If HERDER_GUID was inherited from another session, unset it and retry; if this seat was never enrolled, run `herder enroll` from this pane"
	switch res.Outcome.Status {
	case occupant.PositiveMismatch:
		cause := "the live transcript does not belong to the claimed registry identity"
		if !occupant.ExactEvidence(res.Observation) {
			cause = "cohort-class occupant evidence conflicts with the claimed registry identity, but is not authoritative enough to name an owner"
		}
		return &SenderIdentityRefusal{Cause: cause, Remedy: recovery}
	case occupant.NoOccupant:
		return &SenderIdentityRefusal{Cause: "no live tool occupant was found for the calling process", Remedy: recovery}
	case occupant.OutcomeUnprobeable:
		return &SenderIdentityRefusal{Cause: "the calling process's live occupant is unprobeable", Remedy: "Restore access to the live pane and process tree, then retry"}
	default:
		return &SenderIdentityRefusal{
			Cause:  "the calling process's occupant identity is ambiguous",
			Remedy: "Restart this seat with `herder resume` so SessionStart re-reports the same session id, then retry",
		}
	}
}

func v2Sessions(recs []registry.Record) []v2.SessionRecord {
	latest := registry.LatestByGUID(recs)
	rows := make([]v2.SessionRecord, 0, len(latest))
	for _, rec := range latest {
		var row v2.SessionRecord
		if len(rec.Raw) == 0 || json.Unmarshal(rec.Raw, &row) != nil || row.GUID == "" || row.State == "" {
			row = registry.V2FromRecord(rec, "", rec.State, rec.RecordedAt)
		}
		rows = append(rows, row)
	}
	return rows
}
