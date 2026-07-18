package grokbridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/shellquote"
)

const DefaultOrphanGrace = 2 * time.Minute

type SweepFinding struct {
	Seat        string
	Type        string
	Severity    string
	CauseClass  string
	Detail      string
	Suggested   string
	ObservedAt  string
	ObservedVia []string
}

type orphanCandidate struct {
	Seat        string    `json:"seat"`
	Fingerprint string    `json:"fingerprint"`
	ObservedAt  time.Time `json:"observed_at"`
}

func orphanCandidatePath(stateDir, seat string) string {
	return filepath.Join(SeatDir(stateDir, seat), "orphan-candidate.json")
}

// SweepOrphanSupervisors consumes committed registry state; it never infers
// owner death from process age. Rowless pairs are visible but manual-only.
func SweepOrphanSupervisors(registryPath string, now time.Time, grace time.Duration) ([]SweepFinding, error) {
	if grace <= 0 {
		grace = DefaultOrphanGrace
	}
	stateDir := filepath.Dir(registryPath)
	processes, err := DiscoverSupervisors(stateDir)
	if err != nil {
		return nil, err
	}
	bySeat := map[string][]SupervisorProcess{}
	for _, process := range processes {
		bySeat[process.Seat] = append(bySeat[process.Seat], process)
	}
	seats := make([]string, 0, len(bySeat))
	for seat := range bySeat {
		seats = append(seats, seat)
	}
	sort.Strings(seats)
	var findings []SweepFinding
	for _, seat := range seats {
		group := bySeat[seat]
		current, err := latestSeat(registryPath, seat)
		if err != nil {
			findings = append(findings, sweepRefusal(seat, now, "registry projection unavailable: "+err.Error(), stateDir))
			continue
		}
		if current == nil {
			_ = os.Remove(orphanCandidatePath(stateDir, seat))
			findings = append(findings, SweepFinding{
				Seat: seat, Type: "rowless-grok-bridge-orphan", Severity: "warning", CauseClass: "rowless_bridge_owner_unknown",
				Detail:    fmt.Sprintf("rowless Grok bridge orphan candidate has %d supervisor process(es); no committed owner state exists, so automatic teardown is refused", len(group)),
				Suggested: ManualStopRecipe(stateDir, seat), ObservedAt: now.UTC().Format(time.RFC3339), ObservedVia: []string{"grok_bridge_process_scan", "registry_projection"},
			})
			continue
		}
		if current.Tool != "grok" {
			_ = os.Remove(orphanCandidatePath(stateDir, seat))
			findings = append(findings, sweepRefusal(seat, now, fmt.Sprintf("seat guid belongs to tool %q, not grok", current.Tool), stateDir))
			continue
		}
		if current.State == v2.StateSeated {
			_ = os.Remove(orphanCandidatePath(stateDir, seat))
			continue
		}
		clients, verified, verifyErr := bridgeClientCount(stateDir, seat, owningSessionID(current))
		if verifyErr != nil || !verified {
			_ = os.Remove(orphanCandidatePath(stateDir, seat))
			detail := "live-client absence could not be verified"
			if verifyErr != nil {
				detail += ": " + verifyErr.Error()
			}
			findings = append(findings, sweepRefusal(seat, now, detail, stateDir))
			continue
		}
		if clients != 0 {
			_ = os.Remove(orphanCandidatePath(stateDir, seat))
			findings = append(findings, sweepRefusal(seat, now, fmt.Sprintf("bridge still has %d authenticated client(s)", clients), stateDir))
			continue
		}
		fingerprint := supervisorFingerprint(group)
		candidate, err := readOrphanCandidate(orphanCandidatePath(stateDir, seat))
		if err != nil || candidate.Seat != seat || candidate.Fingerprint != fingerprint {
			candidate = orphanCandidate{Seat: seat, Fingerprint: fingerprint, ObservedAt: now.UTC()}
			if err = writeOrphanCandidate(orphanCandidatePath(stateDir, seat), candidate); err != nil {
				findings = append(findings, sweepRefusal(seat, now, "record orphan grace candidate: "+err.Error(), stateDir))
				continue
			}
		}
		if now.Sub(candidate.ObservedAt) < grace {
			findings = append(findings, SweepFinding{
				Seat: seat, Type: "grok-bridge-orphan-grace", Severity: "warning", CauseClass: "bridge_orphan_grace",
				Detail:    fmt.Sprintf("Grok bridge maps to non-seated row and has no live client; waiting for %s eligibility grace before re-verification", grace),
				Suggested: ManualStopRecipe(stateDir, seat), ObservedAt: now.UTC().Format(time.RFC3339), ObservedVia: []string{"registry_projection", "grok_bridge_status", "grok_bridge_process_scan"},
			})
			continue
		}

		// Re-read every admitting fact immediately before the destructive action.
		confirmed, err := latestSeat(registryPath, seat)
		if err != nil || confirmed == nil || confirmed.Tool != "grok" || confirmed.State == v2.StateSeated {
			_ = os.Remove(orphanCandidatePath(stateDir, seat))
			continue
		}
		currentProcesses, err := DiscoverSupervisors(stateDir)
		if err != nil || supervisorFingerprint(filterSeat(currentProcesses, seat)) != fingerprint {
			_ = os.Remove(orphanCandidatePath(stateDir, seat))
			continue
		}
		clients, verified, err = bridgeClientCount(stateDir, seat, owningSessionID(confirmed))
		if err != nil || !verified || clients != 0 {
			_ = os.Remove(orphanCandidatePath(stateDir, seat))
			continue
		}
		if _, statErr := os.Lstat(SocketPath(stateDir, seat)); statErr == nil {
			client, dialErr := DialClientForSession(SocketPath(stateDir, seat), owningSessionID(confirmed))
			if dialErr != nil {
				findings = append(findings, sweepRefusal(seat, now, "quiesce bridge before teardown: "+dialErr.Error(), stateDir))
				continue
			}
			if _, quiesceErr := client.Call(Request{Op: "quiesce"}); quiesceErr != nil {
				findings = append(findings, sweepRefusal(seat, now, "quiesce bridge before teardown: "+quiesceErr.Error(), stateDir))
				continue
			}
		}
		stopped, err := StopSeatSupervisors(stateDir, seat, DefaultSupervisorStopTimeout)
		if err != nil {
			findings = append(findings, sweepRefusal(seat, now, "automatic bridge teardown failed: "+err.Error(), stateDir))
			continue
		}
		if _, err = RetireOffline(stateDir, seat); err != nil {
			findings = append(findings, sweepRefusal(seat, now, "retire stopped bridge journal: "+err.Error(), stateDir))
			continue
		}
		_ = os.Remove(orphanCandidatePath(stateDir, seat))
		findings = append(findings, SweepFinding{
			Seat: seat, Type: "grok-bridge-orphan-reaped", Severity: "info", CauseClass: "row_confirmed_nonseated_bridge",
			Detail:     fmt.Sprintf("reaped %d Grok bridge supervisor(s) after committed non-seated state, grace, same-incarnation re-verification, and zero live clients", stopped.Matched),
			ObservedAt: now.UTC().Format(time.RFC3339), ObservedVia: []string{"registry_projection", "grok_bridge_status", "grok_bridge_process_scan"},
		})
	}
	return findings, nil
}

func latestSeat(path, seat string) (*v2.SessionRecord, error) {
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return registry.V2ByGUID(projection, seat), nil
}

func bridgeClientCount(stateDir, seat, sessionID string) (int, bool, error) {
	socket := SocketPath(stateDir, seat)
	if _, err := os.Lstat(socket); errors.Is(err, os.ErrNotExist) {
		return 0, true, nil
	} else if err != nil {
		return 0, false, err
	}
	if sessionID == "" {
		return 0, false, errors.New("owning Grok session id is unavailable")
	}
	client, err := DialClientForSession(socket, sessionID)
	if err != nil {
		return 0, false, err
	}
	status, err := client.Call(Request{Op: "status"})
	if err != nil || status.Status == nil {
		return 0, false, err
	}
	return status.Status.Clients, true, nil
}

func owningSessionID(current *v2.SessionRecord) string {
	if current == nil {
		return ""
	}
	if len(current.SIDs) > 0 && current.SIDs[len(current.SIDs)-1].SID != "" {
		return current.SIDs[len(current.SIDs)-1].SID
	}
	return current.Provenance.ToolSessionID
}

func supervisorFingerprint(processes []SupervisorProcess) string {
	parts := make([]string, 0, len(processes))
	for _, process := range processes {
		parts = append(parts, fmt.Sprintf("%d:%s:%d", process.PID, process.StartTime, process.PGID))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func filterSeat(processes []SupervisorProcess, seat string) []SupervisorProcess {
	var out []SupervisorProcess
	for _, process := range processes {
		if process.Seat == seat {
			out = append(out, process)
		}
	}
	return out
}

func readOrphanCandidate(path string) (orphanCandidate, error) {
	var candidate orphanCandidate
	data, err := os.ReadFile(path)
	if err != nil {
		return candidate, err
	}
	err = json.Unmarshal(data, &candidate)
	return candidate, err
}

func writeOrphanCandidate(path string, candidate orphanCandidate) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(candidate)
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}

func sweepRefusal(seat string, now time.Time, detail, stateDir string) SweepFinding {
	return SweepFinding{
		Seat: seat, Type: "grok-bridge-orphan-refused", Severity: "warning", CauseClass: "bridge_orphan_guard_refused",
		Detail: detail + "; automatic teardown refused", Suggested: ManualStopRecipe(stateDir, seat),
		ObservedAt: now.UTC().Format(time.RFC3339), ObservedVia: []string{"grok_bridge_process_scan", "registry_projection"},
	}
}

func ManualStopRecipe(stateDir, seat string) string {
	return "herder grok stop-bridge --seat " + shellquote.Quote(seat) + " --state-dir " + shellquote.Quote(stateDir)
}
