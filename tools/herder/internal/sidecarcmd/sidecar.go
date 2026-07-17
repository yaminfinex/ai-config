package sidecarcmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/launchcmd"
	"ai-config/tools/herder/internal/pendingprompt"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
	"ai-config/tools/herder/internal/send"
)

type options struct {
	tool string
}

type hcomRow struct {
	Name           string           `json:"name"`
	BaseName       string           `json:"base_name"`
	Tag            string           `json:"tag"`
	Directory      string           `json:"directory"`
	Tool           string           `json:"tool"`
	Status         string           `json:"status"`
	Joined         *bool            `json:"joined,omitempty"`
	StatusAgeS     int64            `json:"status_age_seconds"`
	SessionID      string           `json:"session_id"`
	HooksBound     bool             `json:"hooks_bound"`
	TranscriptPath string           `json:"transcript_path"`
	UnreadCount    int64            `json:"unread_count"`
	CreatedAt      flexibleJSONText `json:"created_at"`
	LaunchContext  struct {
		PaneID    string `json:"pane_id"`
		ProcessID string `json:"process_id"`
	} `json:"launch_context"`
}

type flexibleJSONText string

func (t *flexibleJSONText) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = flexibleJSONText(s)
		return nil
	}
	*t = flexibleJSONText(string(b))
	return nil
}

// Run bridges hcom status to herdr's pane.report_agent socket protocol.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		printHelp(stdout)
		return 0
	}
	opts, ok := parseArgs(args)
	if !ok {
		return 1
	}
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_SOCKET_PATH") == "" || os.Getenv("HERDR_PANE_ID") == "" {
		return 0
	}
	sidecar := &sidecar{
		tool:            opts.tool,
		paneID:          os.Getenv("HERDR_PANE_ID"),
		socketPath:      os.Getenv("HERDR_SOCKET_PATH"),
		tag:             os.Getenv("HERDER_ROLE"),
		cwd:             currentCWD(),
		ppid0:           os.Getppid(),
		registry:        registry.DefaultPath(),
		lifecycleMode:   os.Getenv("HERDER_LIFECYCLE_MODE"),
		parentSessionID: os.Getenv("HERDER_PARENT_SESSION_ID"),
		completeSeat:    completeObservedSeat,
		diagnostic:      stderr,
	}
	return sidecar.run()
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder sidecar — internal: bridges hcom status to herdr pane status.

Invoked automatically by herder launch/spawn. Not for direct use.

Usage:
  herder sidecar --tool <tool>
`)
}

func parseArgs(args []string) (options, bool) {
	var opts options
	for i := 0; i < len(args); {
		switch args[i] {
		case "--tool":
			if i+1 >= len(args) {
				return opts, false
			}
			opts.tool = args[i+1]
			i += 2
		default:
			return opts, false
		}
	}
	return opts, opts.tool != ""
}

type sidecar struct {
	tool                 string
	paneID               string
	socketPath           string
	tag                  string
	cwd                  string
	ppid0                int
	registry             string
	lastState            string
	missing              int
	enrichedCorrelated   bool
	enrichedSessionID    string
	lastReportedSID      string
	lifecycleMode        string
	parentSessionID      string
	correlatedProcessID  string
	correlatedName       string
	correlatedPIDs       []int
	processEnvirons      processEnvironmentScanner
	instancePID          func(string, string) (int, error)
	diagnostic           io.Writer
	diagnosticStates     map[string]string
	pidSchemaWarned      bool
	statuslineSnapshots  *statuslineSnapshotWriter
	completeSeat         func(context.Context, *hcomRow, seatcompletion.Request) (seatcompletion.Result, error)
	deliverPrompt        func(string, string, string, string, int) string
	pendingPromptHandled bool
}

type processEnvironmentScanner func(tool string) []processEnvironmentRead

type processEnvironmentRead struct {
	pid int
	env map[string]string
	err error
}

func (s *sidecar) run() int {
	s.report("working")
	s.lastState = "working"

	row, paneCorrelated := s.discoverRow()
	rows := hcomList()
	if row == nil {
		row, paneCorrelated = s.findRowCorrelated(rows)
	}
	s.writeStatuslineSnapshots(rows, row, paneCorrelated)
	s.enrichDiscovered(row, paneCorrelated)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		if os.Getppid() != s.ppid0 {
			s.release(true)
			return 0
		}
		if row == nil {
			s.missing++
		} else {
			s.missing = 0
			// Re-enrichment is gated on a CHILD-SPECIFIC (pane) correlate for the
			// same reason as the initial write: a fallback-only row is not proven
			// ours, so its bus name must never be attached to this guid (TASK-033).
			if s.shouldAppendCorrelatedEnrichment(row, paneCorrelated) {
				_ = s.appendCorrelatedEnrichment(row)
			}
			if paneCorrelated {
				s.deliverPendingPromptForCompletedSeat(row)
			}
			s.reportAgentSession(row, paneCorrelated)
			if state, ok := mapStatus(row.Status); ok && state != s.lastState {
				s.report(state)
				s.lastState = state
			}
		}
		if s.missing >= 5 {
			s.release(false)
			return 0
		}
		<-ticker.C
		rows = hcomList()
		row, paneCorrelated = s.findRowCorrelated(rows)
		s.writeStatuslineSnapshots(rows, row, paneCorrelated)
	}
}

func (s *sidecar) writeStatuslineSnapshots(rows []hcomRow, row *hcomRow, correlated bool) {
	if s.statuslineSnapshots == nil {
		s.statuslineSnapshots = newStatuslineSnapshotWriter(os.Getenv("HCOM_DIR"))
	}
	if correlated && row != nil && row.LaunchContext.ProcessID != "" {
		s.statuslineSnapshots.writeCorrelated(*row, rows, time.Now())
		return
	}
	s.statuslineSnapshots.writeRows(rows, time.Now())
}

func (s *sidecar) removeOwnStatuslineSnapshot() {
	if s.statuslineSnapshots == nil {
		s.statuslineSnapshots = newStatuslineSnapshotWriter(os.Getenv("HCOM_DIR"))
	}
	s.statuslineSnapshots.removeOwned()
}

// enrichDiscovered writes the initial registry enrichment for a freshly
// discovered row, but ONLY when the match is pane-correlated (child-specific).
// A fallback-only (tool+tag+cwd) row is left unwritten: a stale same-tag+cwd
// agent can be the sole match, and attaching its bus name to this guid is the
// wrong-guid enrichment TASK-033 forbids — the sidecar WRITE is row enrichment,
// so AC #1's "never record a tag+cwd-guessed name" binds it too. The manual
// bus name still reaches the registry via `herder enroll` (HCOM_INSTANCE_NAME,
// a genuinely child-specific signal), and a spawned agent's pane correlate
// appears on a later poll → the real name enriches then. Returns whether it
// wrote, so the invariant is unit-testable without run()'s loop.
func (s *sidecar) enrichDiscovered(row *hcomRow, paneCorrelated bool) bool {
	if row == nil || !paneCorrelated {
		return false
	}
	wrote := s.appendCorrelatedEnrichment(row)
	s.reportAgentSession(row, paneCorrelated)
	return wrote
}

func (s *sidecar) shouldAppendCorrelatedEnrichment(row *hcomRow, paneCorrelated bool) bool {
	if row == nil || !paneCorrelated {
		return false
	}
	if !s.enrichedCorrelated {
		return true
	}
	return row.SessionID != "" && (row.SessionID != s.enrichedSessionID || s.latestSessionMissing(row.SessionID))
}

func (s *sidecar) appendCorrelatedEnrichment(row *hcomRow) bool {
	if !s.appendEnrichment(row) {
		return false
	}
	s.enrichedCorrelated = true
	s.enrichedSessionID = row.SessionID
	return true
}

func (s *sidecar) discoverRow() (*hcomRow, bool) {
	for i := 0; i < 90; i++ {
		if os.Getppid() != s.ppid0 {
			s.release(true)
			return nil, false
		}
		if row, paneCorrelated := s.findRowCorrelated(hcomList()); row != nil {
			return row, paneCorrelated
		}
		time.Sleep(700 * time.Millisecond)
	}
	return nil, false
}

func mapStatus(status string) (string, bool) {
	switch status {
	case "active":
		return "working", true
	case "listening":
		return "idle", true
	case "blocked":
		return "blocked", true
	default:
		return "", false
	}
}

func findRowForPane(rows []hcomRow, paneID, lifecycleMode, parentSessionID string) *hcomRow {
	for i := range rows {
		if lifecycleMode == "fork" && parentSessionID != "" && rows[i].SessionID == parentSessionID {
			continue
		}
		if rows[i].LaunchContext.PaneID == paneID {
			return &rows[i]
		}
	}
	return nil
}

func exactProcessIDMatch(rows []hcomRow, processID, lifecycleMode, parentSessionID string) (*hcomRow, int) {
	if processID == "" {
		return nil, 0
	}
	var hit *hcomRow
	count := 0
	for i := range rows {
		if lifecycleMode == "fork" && parentSessionID != "" && rows[i].SessionID == parentSessionID {
			continue
		}
		if sidecarRowJoined(rows[i]) && rows[i].LaunchContext.ProcessID == processID {
			count++
			hit = &rows[i]
		}
	}
	return hit, count
}

func exactStoredNameMatch(rows []hcomRow, storedName, lifecycleMode, parentSessionID string) (*hcomRow, int) {
	if storedName == "" {
		return nil, 0
	}
	var hit *hcomRow
	count := 0
	for i := range rows {
		if lifecycleMode == "fork" && parentSessionID != "" && rows[i].SessionID == parentSessionID {
			continue
		}
		if !sidecarRowJoined(rows[i]) || !hcomidentity.StoredNameMatches(rows[i].Name, rows[i].BaseName, storedName) {
			continue
		}
		count++
		if hit == nil {
			hit = &rows[i]
		}
	}
	return hit, count
}

func sidecarRowJoined(row hcomRow) bool {
	if row.Name == "" {
		return false
	}
	if row.Joined != nil && !*row.Joined {
		return false
	}
	switch strings.ToLower(row.Status) {
	case "inactive", "stopped", "closed", "dead":
		return false
	default:
		return true
	}
}

type ownedChildIdentity struct {
	StoredName string
	ProcessID  string
	PIDs       []int
}

func (i ownedChildIdentity) empty() bool {
	return i.StoredName == "" && i.ProcessID == "" && len(i.PIDs) == 0
}

func findRowForOwnedChild(
	rows []hcomRow,
	identity ownedChildIdentity,
	lifecycleMode, parentSessionID string,
	pidCorroborates func(hcomRow) bool,
) *hcomRow {
	byName, nameCount := exactStoredNameMatch(rows, identity.StoredName, lifecycleMode, parentSessionID)
	byProcess, processCount := exactProcessIDMatch(rows, identity.ProcessID, lifecycleMode, parentSessionID)
	if nameCount > 1 || processCount > 1 {
		return nil
	}
	if byName != nil && byProcess != nil && byName.Name != byProcess.Name {
		return nil
	}
	if byProcess != nil {
		return byProcess
	}
	if byName != nil && pidCorroborates != nil && pidCorroborates(*byName) {
		return byName
	}
	return nil
}

// findRowCorrelated locates this session's hcom row and reports whether the
// match is CHILD-SPECIFIC. A pane correlate or the exact hcom name/process id
// read from a live tool process carrying this HERDER_GUID identifies THIS
// session's row. The tool+tag+cwd launch fallback does NOT — during a window
// where our own row has no pane/name/process correlate,
// a STALE same-tag+cwd agent can be the sole match. The fallback row is still
// returned (status bridging keeps using it), flagged paneCorrelated=false so
// callers never write its bus name onto this guid (TASK-033: row enrichment
// must never record a tag+cwd-guessed name — the sidecar WRITE is row
// enrichment). On a fallback-only tick the name is simply left unwritten; the
// sidecar re-fires each poll, so once the pane correlate appears the real name
// is enriched then (natural retry).
func (s *sidecar) findRowCorrelated(rows []hcomRow) (row *hcomRow, paneCorrelated bool) {
	if r := findRowForPane(rows, s.paneID, s.lifecycleMode, s.parentSessionID); r != nil {
		s.noteCorrelation("pane", len(rows), r)
		return r, true
	}
	cached := ownedChildIdentity{StoredName: s.correlatedName, ProcessID: s.correlatedProcessID, PIDs: s.correlatedPIDs}
	if r := findRowForOwnedChild(rows, cached, s.lifecycleMode, s.parentSessionID, s.pidCorroborator(cached)); r != nil {
		s.noteCorrelation("cached-child", len(rows), r)
		return r, true
	}
	// Reading a LIVE child process environ is authoritative for this spawned
	// child and not the TASK-043 inherited shell env hazard: HERDER_GUID proves
	// ownership. Every matching process must agree on the base-form
	// HCOM_INSTANCE_NAME and HCOM_PROCESS_ID. A stored-name match is only a clue:
	// it must be corroborated by the row's process id or by the hcom row's live
	// OS process carrying this HERDER_GUID.
	if identity := s.findIdentityForOwnChild(); !identity.empty() {
		if r := findRowForOwnedChild(rows, identity, s.lifecycleMode, s.parentSessionID, s.pidCorroborator(identity)); r != nil {
			s.correlatedName = r.Name
			s.correlatedProcessID = identity.ProcessID
			s.correlatedPIDs = append([]int(nil), identity.PIDs...)
			s.noteCorrelation("owned-process", len(rows), r)
			return r, true
		}
	}
	fallback := findRowForLaunchFallback(rows, s.tool, s.tag, s.cwd, s.lifecycleMode, s.parentSessionID)
	source := "none"
	if fallback != nil {
		source = "untrusted-fallback"
	}
	s.noteCorrelation(source, len(rows), fallback)
	return fallback, false
}

func (s *sidecar) findRow(rows []hcomRow) *hcomRow {
	row, _ := s.findRowCorrelated(rows)
	return row
}

func (s *sidecar) appendEnrichment(row *hcomRow) bool {
	if s.tool == "pi" && !piRowBound(row) {
		s.diagTransition("completion-result", "completion refused: Pi bind predicate incomplete")
		return false
	}
	s.diagTransition("completion-attempt", "completion attempt: correlated joined row observed")
	guid, hadGUID := os.LookupEnv("HERDER_GUID")
	recs, _ := registry.Load(s.registry)
	resumed := false
	var latest *registry.Record
	if guid == "" {
		if row.SessionID != "" {
			latest = registry.ResolveByToolSessionID(recs, row.SessionID)
			if latest != nil {
				guid = ptrString(latest.GUID)
				resumed = guid != ""
			}
		}
		if guid == "" {
			var err error
			guid, err = registry.NewGUID()
			if err != nil {
				return false
			}
		}
	}
	short := registry.ShortGUID(guid)
	coords := s.paneCoordinates()
	if coords.PaneID == "" {
		coords.PaneID = s.paneID
	}
	if coords.CWD == "" {
		coords.CWD = firstNonEmpty(row.Directory, s.cwd)
	}

	if latest == nil {
		latest = s.latestFromRecords(recs, guid)
	}
	if latest == nil {
		latest = s.archivedLatest(guid)
	}
	if s.tool == "pi" && (latest == nil || latest.Provider == "") {
		return false
	}
	if latest != nil && registry.IsTerminal(*latest) {
		s.diagTransition("completion-result", "completion refused: registry session is terminal")
		return false
	}
	if completedRecognitionMatches(latest, row, coords) {
		// Spawn (or an earlier sidecar pass) already completed this exact
		// canonical seat. This is a successful idempotent replay: no second row.
		s.diagTransition("completion-result", "completion result: canonical seat already present")
		s.deliverPendingPrompt(row)
		return true
	}
	label := os.Getenv("HERDER_LABEL")
	role := os.Getenv("HERDER_ROLE")
	agent := s.tool
	if latest != nil {
		label = firstNonEmpty(ptrString(latest.Label), label)
		role = firstNonEmpty(latest.Role, role)
		agent = firstNonEmpty(latest.Agent, agent)
		coords.TerminalID = firstNonEmpty(coords.TerminalID, latest.TerminalID)
		coords.PaneID = firstNonEmpty(coords.PaneID, latest.PaneID)
	}
	if label == "" {
		label = "manual-" + short
	}
	if role == "" {
		role = "manual"
	}
	if owner := registry.NonRetiredLabelOwner(recs, label, guid); owner != nil {
		s.diagTransition("completion-result", "completion refused: label is owned by another session")
		return false
	}

	mechanism := "enroll"
	switch {
	case os.Getenv("HERDER_SHIM") == "1":
		mechanism = "shim"
	case hadGUID:
		mechanism = "spawn"
	}
	if latest != nil && latest.Provenance != nil && latest.Provenance.Mechanism != "" {
		mechanism = latest.Provenance.Mechanism
	}
	prov := registry.BuildProvenance(mechanism, "", row.SessionID, row.Tag, coords.CWD, coords.WorkspaceID)
	if latest != nil && latest.Provenance != nil {
		carried := *latest.Provenance
		carried.ToolSessionID = prov.ToolSessionID
		carried.CWD = prov.CWD
		carried.WorkspaceID = prov.WorkspaceID
		carried.Branch = prov.Branch
		carried.TS = prov.TS
		prov = carried
	}
	if prov.ToolSessionID == "" {
		prov = registry.PreserveToolSessionID(prov, recs, guid)
	}
	if resumed {
		prov.ResumedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}
	var vendorVersion any
	if s.tool == "pi" {
		observation, observeErr := launchcmd.ObservePiVendorVersion(time.Now())
		if observeErr != nil {
			return false
		}
		vendorVersion = launchcmd.RefreshPiVendorVersion(latest.VendorVersion, observation)
	}

	base := []byte(`{}`)
	if latest != nil && len(bytes.TrimSpace(latest.Raw)) > 0 {
		base = latest.Raw
	}
	if resumed {
		base = registry.DropRawFields(base, "closed_at", "closed_by_pane", "close_result", "close_reason")
	}
	out, err := registry.UpdateRawObject(base, map[string]any{
		"guid":            guid,
		"short_guid":      short,
		"label":           label,
		"role":            role,
		"agent":           agent,
		"pane_id":         coords.PaneID,
		"terminal_id":     coords.TerminalID,
		"workspace_id":    coords.WorkspaceID,
		"cwd":             coords.CWD,
		"hcom_dir":        os.Getenv("HCOM_DIR"),
		"hcom_name":       row.Name,
		"hcom_verified":   true,
		"hooks_bound":     row.HooksBound,
		"transcript_path": row.TranscriptPath,
		"hcom_tag":        row.Tag,
		"status":          "active",
		"provenance":      prov,
	})
	if err == nil && s.tool == "pi" {
		out, err = registry.UpdateRawObject(out, map[string]any{
			"provider":       latest.Provider,
			"model":          latest.Model,
			"vendor_version": vendorVersion,
		})
	}
	if err == nil {
		candidate, decodeErr := registry.SessionEventFromJSON(out, "seated", v2.StateSeated)
		if decodeErr != nil {
			return false
		}
		request := seatcompletion.Request{
			Origin:       seatcompletion.OriginRecognition,
			RegistryPath: s.registry,
			Candidate:    candidate,
			Seat:         seatcompletion.SeatClaim{Kind: seatcompletion.SeatHerdr, PaneID: coords.PaneID, TerminalID: coords.TerminalID},
			Namespace:    os.Getenv("HCOM_DIR"),
			Evidence: hcomidentity.Evidence{
				Name:      s.correlatedName,
				SessionID: row.SessionID,
				ProcessID: firstNonEmpty(s.correlatedProcessID, row.LaunchContext.ProcessID),
				PaneIDs:   []string{coords.PaneID, row.LaunchContext.PaneID},
			},
		}
		complete := s.completeSeat
		if complete == nil {
			complete = completeObservedSeat
		}
		result, completeErr := complete(context.Background(), row, request)
		if completeErr != nil {
			s.diagTransition("completion-result", "completion error: "+completeErr.Error())
			return false
		}
		if result.Refusal != nil {
			s.diagTransition("completion-result", fmt.Sprintf("completion refused [%s]: %s", result.Refusal.Code, result.Refusal.Cause))
			return false
		}
		if result.Status == registry.WriteApplied {
			s.diagTransition("completion-result", "completion result: canonical seat applied")
			s.deliverPendingPrompt(row)
			return true
		}
		if result.Status == registry.WriteNoop {
			confirmed, loadErr := registry.Load(s.registry)
			matched := loadErr == nil && completedRecognitionMatches(s.latestFromRecords(confirmed, guid), row, coords)
			if matched {
				s.diagTransition("completion-result", "completion result: verified canonical noop")
				s.deliverPendingPrompt(row)
				return true
			}
			s.diagTransition("completion-result", "completion refused: unverified registry noop")
			return false
		}
	}
	s.diagTransition("completion-result", "completion produced no successful canonical outcome")
	return false
}

func (s *sidecar) deliverPendingPrompt(row *hcomRow) {
	if s.pendingPromptHandled || row == nil || row.Name == "" {
		return
	}
	guid := os.Getenv("HERDER_GUID")
	result, err := pendingprompt.Attempt(s.registry, guid, "", pendingprompt.ActorSidecar, time.Now().UTC(), func(pending pendingprompt.Record) string {
		deliver := s.deliverPrompt
		if deliver == nil {
			deliver = send.DeliverBus
		}
		return deliver(pending.Sender, row.Name, pending.BusDir, pending.Message, pending.VerifyMS)
	})
	if err != nil {
		s.diagTransition("prompt", "pending prompt error: "+err.Error())
		return
	}
	if !result.Managed {
		return
	}
	if result.Suppressed {
		s.pendingPromptHandled = true
		s.diagTransition("prompt", "pending prompt result: matching manual delivery already completed; duplicate suppressed")
		return
	}
	s.diagTransition("prompt", "pending prompt result: "+result.Verdict)
	if result.Verdict == "delivered" || result.Verdict == "queued" {
		s.pendingPromptHandled = true
	}
}

func (s *sidecar) deliverPendingPromptForCompletedSeat(row *hcomRow) {
	if s.pendingPromptHandled || row == nil {
		return
	}
	guid := os.Getenv("HERDER_GUID")
	coords := s.paneCoordinates()
	if coords.PaneID == "" {
		coords.PaneID = s.paneID
	}
	if latest := s.latest(guid); completedRecognitionMatches(latest, row, coords) {
		s.deliverPendingPrompt(row)
	}
}

func completedRecognitionMatches(latest *registry.Record, row *hcomRow, coords paneCoordinates) bool {
	if latest == nil || row == nil || !registry.IsSeated(*latest) || latest.HcomVerified == nil || !*latest.HcomVerified {
		return false
	}
	if row.SessionID != "" && (latest.Provenance == nil || latest.Provenance.ToolSessionID != row.SessionID) {
		return false
	}
	if row.Tag != "" && latest.HcomTag != row.Tag {
		return false
	}
	hooksBound := latest.HooksBound != nil && *latest.HooksBound
	return latest.PaneID == coords.PaneID && latest.TerminalID == coords.TerminalID &&
		latest.HcomName == row.Name && latest.HcomDir == os.Getenv("HCOM_DIR") &&
		hooksBound == row.HooksBound &&
		latest.TranscriptPath == row.TranscriptPath
}

func completeObservedSeat(ctx context.Context, _ *hcomRow, request seatcompletion.Request) (seatcompletion.Result, error) {
	if request.Seat.Kind == seatcompletion.SeatHerdr {
		terminalID := request.Seat.TerminalID
		if terminalID == "" {
			terminalID = "untracked-terminal"
		}
		request.ObservedPane = &seatcompletion.LivePane{PaneID: request.Seat.PaneID, TerminalID: terminalID}
	}
	return seatcompletion.Complete(ctx, request)
}

func piRowBound(row *hcomRow) bool {
	return row != nil && row.Tool == "pi" && row.HooksBound && strings.TrimSpace(row.SessionID) != ""
}

type paneCoordinates struct {
	PaneID      string
	TerminalID  string
	WorkspaceID string
	CWD         string
}

func (s *sidecar) paneCoordinates() paneCoordinates {
	out, err := (&herdrcli.Client{}).Output("pane", "get", s.paneID)
	if err != nil {
		return paneCoordinates{PaneID: s.paneID}
	}
	pane, err := herdrcli.ParsePaneGet(out)
	if err != nil {
		return paneCoordinates{PaneID: s.paneID}
	}
	return paneCoordinates{
		PaneID:      firstNonEmpty(pane.PaneID, s.paneID),
		TerminalID:  pane.TerminalID,
		WorkspaceID: pane.WorkspaceID,
		CWD:         pane.CWD,
	}
}

func (s *sidecar) latest(guid string) *registry.Record {
	recs, err := registry.Load(s.registry)
	if err != nil {
		return nil
	}
	return s.latestFromRecords(recs, guid)
}

func (s *sidecar) latestFromRecords(recs []registry.Record, guid string) *registry.Record {
	for _, rec := range registry.LatestByGUID(recs) {
		if ptrString(rec.GUID) == guid {
			cp := rec
			return &cp
		}
	}
	return nil
}

func (s *sidecar) archivedLatest(guid string) *registry.Record {
	if guid == "" {
		return nil
	}
	recs, err := registry.LoadArchives(s.registry)
	if err != nil {
		return nil
	}
	return s.latestFromRecords(recs, guid)
}

func (s *sidecar) latestSessionMissing(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	recs, err := registry.Load(s.registry)
	if err != nil {
		return false
	}
	rec := registry.ResolveByToolSessionID(recs, sessionID)
	if rec == nil || rec.Provenance == nil {
		return false
	}
	return rec.Provenance.ToolSessionID != sessionID
}

func findRowForLaunchFallback(rows []hcomRow, tool, tag, cwd, lifecycleMode, parentSessionID string) *hcomRow {
	if tool == "" || tag == "" || cwd == "" {
		return nil
	}
	var hit *hcomRow
	matches := 0
	for i := range rows {
		if rows[i].Status == "inactive" {
			continue
		}
		if lifecycleMode == "fork" && parentSessionID != "" && rows[i].SessionID == parentSessionID {
			continue
		}
		if rows[i].Tool == tool && rows[i].Tag == tag && rows[i].Directory == cwd {
			hit = &rows[i]
			matches++
		}
	}
	// A tag+cwd match is only a trustworthy correlate when it is UNAMBIGUOUS.
	// With two or more live agents sharing tool+tag+cwd there is no positive
	// signal (pane_id/terminal_id/guid) to say which row is ours; picking the
	// newest silently attaches an unrelated agent's identity to this pane's
	// registry guid — the wrong-guid enrichment bug. Refuse to guess.
	if matches != 1 {
		return nil
	}
	return hit
}

func currentCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *sidecar) findIdentityForOwnChild() ownedChildIdentity {
	guid := os.Getenv("HERDER_GUID")
	if guid == "" {
		return ownedChildIdentity{}
	}
	scan := s.processEnvirons
	if scan == nil {
		scan = scanProcessEnvirons
	}
	// Multiple live processes carrying this HERDER_GUID must agree on the hcom
	// name and process id. Disagreement fails closed instead of letting /proc
	// scan order choose an identity.
	identity := ownedChildIdentity{}
	reads := scan(s.tool)
	owned := 0
	for _, read := range reads {
		if read.err != nil {
			continue
		}
		if read.env["HERDER_GUID"] != guid {
			continue
		}
		owned++
		candidate := ownedChildIdentity{
			StoredName: read.env["HCOM_INSTANCE_NAME"],
			ProcessID:  read.env["HCOM_PROCESS_ID"],
		}
		if read.pid > 0 {
			candidate.PIDs = []int{read.pid}
		}
		if candidate.empty() {
			continue
		}
		if identity.StoredName != "" && candidate.StoredName != "" && identity.StoredName != candidate.StoredName {
			s.diagTransition("process-scan", "process scan: owned identity conflict=name")
			return ownedChildIdentity{}
		}
		if identity.ProcessID != "" && candidate.ProcessID != "" && identity.ProcessID != candidate.ProcessID {
			s.diagTransition("process-scan", "process scan: owned identity conflict=process")
			return ownedChildIdentity{}
		}
		identity.StoredName = firstNonEmpty(identity.StoredName, candidate.StoredName)
		identity.ProcessID = firstNonEmpty(identity.ProcessID, candidate.ProcessID)
		if read.pid > 0 && !containsPID(identity.PIDs, read.pid) {
			identity.PIDs = append(identity.PIDs, read.pid)
		}
	}
	s.diagTransition("process-scan", fmt.Sprintf("process scan: owned=%t identity=%t", owned > 0, !identity.empty()))
	return identity
}

func (s *sidecar) noteCorrelation(source string, rows int, row *hcomRow) {
	joined := row != nil && sidecarRowJoined(*row)
	s.diagTransition("correlation", fmt.Sprintf("roster scan: available=%t correlate=%s joined=%t", rows > 0, source, joined))
}

func (s *sidecar) diagTransition(key, message string) {
	if s.diagnostic == nil || message == "" {
		return
	}
	if s.diagnosticStates == nil {
		s.diagnosticStates = make(map[string]string)
	}
	if s.diagnosticStates[key] == message {
		return
	}
	s.diagnosticStates[key] = message
	fmt.Fprintln(s.diagnostic, "herder sidecar: "+message)
}

func (s *sidecar) pidCorroborator(identity ownedChildIdentity) func(hcomRow) bool {
	return func(row hcomRow) bool {
		if len(identity.PIDs) == 0 {
			return false
		}
		baseName := row.BaseName
		if baseName == "" && row.Tag == "" {
			baseName = row.Name
		}
		if baseName == "" {
			return false
		}
		lookup := s.instancePID
		if lookup == nil {
			lookup = hcomidentity.InstancePID
		}
		pid, err := lookup(os.Getenv("HCOM_DIR"), baseName)
		if err != nil {
			if errors.Is(err, hcomidentity.ErrInstancePIDSchemaDrift) {
				s.reportPIDSchemaDrift(err)
			}
			return false
		}
		return pid > 0 && containsPID(identity.PIDs, pid)
	}
}

func (s *sidecar) reportPIDSchemaDrift(err error) {
	if s.pidSchemaWarned {
		return
	}
	s.pidSchemaWarned = true
	diagnostic := s.diagnostic
	if diagnostic == nil {
		diagnostic = os.Stderr
	}
	fmt.Fprintf(diagnostic, "herder sidecar: %v; refusing exact-name recovery\n", err)
}

func containsPID(pids []int, want int) bool {
	for _, pid := range pids {
		if pid == want {
			return true
		}
	}
	return false
}

func scanProcessEnvirons(tool string) []processEnvironmentRead {
	if tool == "" {
		return nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	ownPID := os.Getpid()
	var reads []processEnvironmentRead
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == ownPID {
			continue
		}
		procDir := "/proc/" + entry.Name()
		if !processLooksLikeTool(procDir, tool) {
			continue
		}
		env, err := readProcessEnviron(procDir + "/environ")
		reads = append(reads, processEnvironmentRead{pid: pid, env: env, err: err})
	}
	return reads
}

func processLooksLikeTool(procDir, tool string) bool {
	needle := strings.ToLower(tool)
	for _, name := range []string{"comm", "cmdline"} {
		b, err := os.ReadFile(procDir + "/" + name)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(b)), needle) {
			return true
		}
	}
	return false
}

func readProcessEnviron(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	env := make(map[string]string)
	for _, part := range bytes.Split(b, []byte{0}) {
		if len(part) == 0 {
			continue
		}
		key, value, ok := bytes.Cut(part, []byte{'='})
		if !ok {
			continue
		}
		env[string(key)] = string(value)
	}
	return env, nil
}

func hcomList() []hcomRow {
	cmd := exec.Command("hcom", "list", "--json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil
	}
	var rows []hcomRow
	if json.Unmarshal(stdout.Bytes(), &rows) != nil {
		return nil
	}
	return rows
}

func (s *sidecar) report(state string) {
	_ = s.send("pane.report_agent", map[string]any{
		"pane_id": s.paneID,
		"source":  "herder:sidecar",
		"agent":   s.tool,
		"state":   state,
		"seq":     time.Now().UnixNano(),
	})
}

func (s *sidecar) reportAgentSession(row *hcomRow, paneCorrelated bool) {
	if row == nil || !paneCorrelated || s.paneID == "" || row.SessionID == "" || row.SessionID == s.lastReportedSID {
		return
	}
	// `--source` is the reporter identity used by herdr to order/replace this
	// reporter's session info, not the agent's start cause. `--seq` is optional
	// stale-report protection for multiple reports from one source; the sidecar
	// reports only after its pane-correlated hcom sid changes, so omitting it
	// avoids inventing sequence semantics beyond herdr's accepted CLI contract.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "herdr", "pane", "report-agent-session", s.paneID,
		"--source", "herder:sidecar",
		"--agent", firstNonEmpty(row.Tool, s.tool),
		"--agent-session-id", row.SessionID)
	if err := cmd.Run(); err != nil {
		return
	}
	s.lastReportedSID = row.SessionID
}

func (s *sidecar) release(removeSnapshot bool) {
	if removeSnapshot {
		s.removeOwnStatuslineSnapshot()
	}
	_ = s.send("pane.release_agent", map[string]any{
		"pane_id": s.paneID,
		"source":  "herder:sidecar",
		"agent":   s.tool,
		"seq":     time.Now().UnixNano(),
	})
}

func (s *sidecar) send(method string, params map[string]any) error {
	conn, err := net.Dial("unix", s.socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	req := map[string]any{
		"id":     fmt.Sprintf("herder:sidecar:%d", time.Now().UnixNano()),
		"method": method,
		"params": params,
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	_, _ = bufio.NewReader(conn).ReadBytes('\n')
	return nil
}
