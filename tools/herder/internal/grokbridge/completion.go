package grokbridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/pendingprompt"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
	"ai-config/tools/herder/internal/send"
)

type managedCompletionConfig struct {
	Seat      string
	StateDir  string
	HcomDir   string
	SessionID string
	PaneID    string
}

func superviseManagedCompletion(ctx context.Context, cfg managedCompletionConfig, logf func(string, ...any)) {
	if cfg.Seat == "" || cfg.StateDir == "" || cfg.SessionID == "" || cfg.PaneID == "" {
		logf("managed seat completion disabled: required seat/session/pane coordinate is absent")
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		done, err := completeManagedSeat(ctx, cfg)
		if done {
			if err != nil {
				logf("managed seat completion stopped: %v", err)
			} else {
				logf("managed seat completion and pending-prompt handoff converged")
			}
			return
		}
		if err != nil {
			logf("managed seat completion waiting: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func completeManagedSeat(ctx context.Context, cfg managedCompletionConfig) (bool, error) {
	registryPath := filepath.Join(cfg.StateDir, "registry.jsonl")
	paneOut, err := (&herdrcli.Client{}).Output("pane", "get", cfg.PaneID)
	if err != nil {
		return false, fmt.Errorf("resolve live Grok pane: %w", err)
	}
	pane, err := herdrcli.ParsePaneGet(paneOut)
	if err != nil || pane.PaneID == "" || pane.TerminalID == "" {
		return false, fmt.Errorf("resolve live Grok pane coordinates: %w", err)
	}

	busNameData, err := os.ReadFile(filepath.Join(SeatDir(cfg.StateDir, cfg.Seat), "bus-name"))
	if err != nil {
		return false, fmt.Errorf("read bridge-owned bus coordinate: %w", err)
	}
	baseName := strings.TrimSpace(string(busNameData))
	rows, err := hcomidentity.ListContext(ctx, cfg.HcomDir)
	if err != nil {
		return false, fmt.Errorf("list joined bridge bus rows: %w", err)
	}
	joined, count := hcomidentity.JoinedStoredCount(rows, baseName)
	if count != 1 {
		return false, fmt.Errorf("authoritative bridge BaseName %q resolves to %d joined rows", baseName, count)
	}
	authoritativeBase := joined.BaseName
	if authoritativeBase == "" {
		authoritativeBase = joined.Name
	}
	if authoritativeBase != baseName {
		return false, fmt.Errorf("joined bridge row BaseName %q does not match durable coordinate %q", authoritativeBase, baseName)
	}
	if joined.LaunchContext.ProcessID != cfg.Seat {
		return false, fmt.Errorf("joined bridge row process coordinate %q does not match seat %q", joined.LaunchContext.ProcessID, cfg.Seat)
	}

	current, err := latestSeat(registryPath, cfg.Seat)
	if err != nil {
		return false, err
	}
	if current != nil && current.State != v2.StateSeated {
		return true, fmt.Errorf("managed completion refused: latest Grok row is %s, not a birth-side missing/seated row", current.State)
	}
	if canonicalManagedSeatMatches(current, cfg, pane, joined.Name) {
		return deliverManagedPendingPrompt(registryPath, cfg.Seat, joined.Name)
	}

	candidate := managedCompletionCandidate(cfg, pane, joined.Name)
	engine := seatcompletion.DefaultEngine()
	engine.UpdateRegistry = func(path string, update registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
		return registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
			latest := registry.V2ByGUID(tx.Projection, cfg.Seat)
			if canonicalManagedSeatMatches(latest, cfg, pane, joined.Name) {
				replay := *latest
				replay.Event = "registered"
				replay.RecordedAt = ""
				replay.Raw = nil
				return []v2.SessionRecord{replay}, nil
			}
			if latest != nil && latest.State != v2.StateSeated {
				return nil, errors.New("managed completion lost race to a non-seated owner state")
			}
			return update(tx)
		})
	}
	result, err := engine.Complete(ctx, seatcompletion.Request{
		Origin: seatcompletion.OriginRecognition, RegistryPath: registryPath, Candidate: candidate,
		Seat: seatcompletion.SeatClaim{Kind: seatcompletion.SeatHerdr, PaneID: pane.PaneID}, Namespace: cfg.HcomDir, RequireBus: true,
		Evidence: hcomidentity.Evidence{
			Name: joined.Name, SessionID: joined.SessionID, ProcessID: cfg.Seat,
			PaneIDs: []string{pane.PaneID, joined.LaunchContext.PaneID},
		},
	})
	if err != nil {
		return false, err
	}
	if result.Refusal != nil {
		return false, fmt.Errorf("canonical completion refused [%s]: %s", result.Refusal.Code, result.Refusal.Cause)
	}
	if result.Status != registry.WriteApplied && result.Status != registry.WriteNoop {
		return false, fmt.Errorf("canonical completion status is %s", result.Status)
	}
	confirmed, err := latestSeat(registryPath, cfg.Seat)
	if err != nil || !canonicalManagedSeatMatches(confirmed, cfg, pane, joined.Name) {
		return false, errors.New("canonical completion outcome has no exact seated row")
	}
	return deliverManagedPendingPrompt(registryPath, cfg.Seat, joined.Name)
}

func managedCompletionCandidate(cfg managedCompletionConfig, pane herdrcli.Pane, hcomName string) v2.SessionRecord {
	guid := cfg.Seat
	label := os.Getenv("HERDER_LABEL")
	role := os.Getenv("HERDER_ROLE")
	if label == "" {
		label = "grok-" + registry.ShortGUID(guid)
	}
	if role == "" {
		role = "worker"
	}
	workspace := pane.WorkspaceID
	cwd := pane.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	provenance := registry.BuildProvenance("spawn", os.Getenv("HERDER_SPAWNED_BY"), cfg.SessionID, role, cwd, workspace)
	hooksBound := false
	record := registry.Record{
		GUID: &guid, ShortGUID: stringPtr(registry.ShortGUID(guid)), Label: &label, Role: role, Agent: "grok",
		PaneID: pane.PaneID, TerminalID: pane.TerminalID, HcomDir: cfg.HcomDir, HcomName: hcomName,
		HcomTag: role, HooksBound: &hooksBound, Status: "active", Provenance: &provenance,
	}
	if slug := os.Getenv("HERDER_MISSION_SLUG"); slug != "" {
		record.Mission = &v2.Mission{Slug: slug, Source: os.Getenv("HERDER_MISSION_SOURCE")}
	}
	return registry.V2FromRecord(record, "seated", v2.StateSeated, time.Now().UTC().Format(time.RFC3339))
}

func canonicalManagedSeatMatches(current *v2.SessionRecord, cfg managedCompletionConfig, pane herdrcli.Pane, hcomName string) bool {
	if current == nil || current.Tool != "grok" || current.State != v2.StateSeated || current.Seat == nil {
		return false
	}
	seat := current.Seat
	return current.Provenance.ToolSessionID == cfg.SessionID && seat.Kind == seatcompletion.SeatHerdr &&
		seat.PaneID == pane.PaneID && seat.TerminalID == pane.TerminalID && seat.Namespace == cfg.HcomDir &&
		seat.HcomName == hcomName && seat.HcomVerified != nil && *seat.HcomVerified
}

func deliverManagedPendingPrompt(registryPath, guid, hcomName string) (bool, error) {
	result, err := pendingprompt.Attempt(registryPath, guid, "", pendingprompt.ActorSidecar, time.Now().UTC(), func(pending pendingprompt.Record) string {
		return send.DeliverBus(pending.Sender, hcomName, pending.BusDir, pending.Message, pending.VerifyMS)
	})
	if err != nil {
		return false, err
	}
	if !result.Managed || result.Suppressed || result.Verdict == "delivered" || result.Verdict == "queued" {
		return true, nil
	}
	return false, fmt.Errorf("pending prompt delivery result: %s", result.Verdict)
}

func stringPtr(value string) *string { return &value }
