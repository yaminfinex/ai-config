package sidecarcmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type options struct {
	tool string
}

type hcomRow struct {
	Name          string           `json:"name"`
	Tag           string           `json:"tag"`
	Directory     string           `json:"directory"`
	Tool          string           `json:"tool"`
	Status        string           `json:"status"`
	SessionID     string           `json:"session_id"`
	CreatedAt     flexibleJSONText `json:"created_at"`
	LaunchContext struct {
		PaneID string `json:"pane_id"`
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
	_ = stdout
	_ = stderr
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
	}
	return sidecar.run()
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
	tool              string
	paneID            string
	socketPath        string
	tag               string
	cwd               string
	ppid0             int
	registry          string
	lastState         string
	missing           int
	enrichedSessionID string
	lifecycleMode     string
	parentSessionID   string
}

func (s *sidecar) run() int {
	s.report("working")
	s.lastState = "working"

	row := s.discoverRow()
	if row != nil {
		s.appendEnrichment(row)
		s.enrichedSessionID = row.SessionID
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		if os.Getppid() != s.ppid0 {
			s.release()
			return 0
		}
		if row == nil {
			s.missing++
		} else {
			s.missing = 0
			if row.SessionID != "" && (row.SessionID != s.enrichedSessionID || s.latestSessionMissing(row.SessionID)) {
				s.appendEnrichment(row)
				s.enrichedSessionID = row.SessionID
			}
			if state, ok := mapStatus(row.Status); ok && state != s.lastState {
				s.report(state)
				s.lastState = state
			}
		}
		if s.missing >= 5 {
			s.release()
			return 0
		}
		<-ticker.C
		row = s.findRow(hcomList())
	}
}

func (s *sidecar) discoverRow() *hcomRow {
	for i := 0; i < 90; i++ {
		if os.Getppid() != s.ppid0 {
			s.release()
			return nil
		}
		if row := s.findRow(hcomList()); row != nil {
			return row
		}
		time.Sleep(700 * time.Millisecond)
	}
	return nil
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

func findRowForPane(rows []hcomRow, paneID string) *hcomRow {
	for i := range rows {
		if rows[i].LaunchContext.PaneID == paneID {
			return &rows[i]
		}
	}
	return nil
}

func (s *sidecar) findRow(rows []hcomRow) *hcomRow {
	if row := findRowForPane(rows, s.paneID); row != nil {
		return row
	}
	return findRowForLaunchFallback(rows, s.tool, s.tag, s.cwd, s.lifecycleMode, s.parentSessionID)
}

func (s *sidecar) appendEnrichment(row *hcomRow) {
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
				return
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
	prov := registry.BuildProvenance(mechanism, row.Tag, coords.CWD, coords.WorkspaceID)
	if latest != nil && latest.Provenance != nil {
		carried := *latest.Provenance
		carried.CWD = prov.CWD
		carried.WorkspaceID = prov.WorkspaceID
		carried.Branch = prov.Branch
		carried.TS = prov.TS
		prov = carried
	}
	prov.ToolSessionID = row.SessionID
	if prov.ToolSessionID == "" {
		prov = registry.PreserveToolSessionID(prov, recs, guid)
	}
	if resumed {
		prov.ResumedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}

	base := []byte(`{}`)
	if latest != nil && len(bytes.TrimSpace(latest.Raw)) > 0 {
		base = latest.Raw
	}
	if resumed {
		base = registry.DropRawFields(base, "closed_at", "closed_by_pane", "close_result", "close_reason")
	}
	out, err := registry.UpdateRawObject(base, map[string]any{
		"guid":         guid,
		"short_guid":   short,
		"label":        label,
		"role":         role,
		"agent":        agent,
		"pane_id":      coords.PaneID,
		"terminal_id":  coords.TerminalID,
		"workspace_id": coords.WorkspaceID,
		"cwd":          coords.CWD,
		"hcom_dir":     os.Getenv("HCOM_DIR"),
		"hcom_name":    row.Name,
		"hcom_tag":     row.Tag,
		"status":       "active",
		"provenance":   prov,
	})
	if err == nil {
		_ = registry.Append(s.registry, out)
	}
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
	for i := range rows {
		if rows[i].Status == "inactive" {
			continue
		}
		if lifecycleMode == "fork" && parentSessionID != "" && rows[i].SessionID == parentSessionID {
			continue
		}
		if rows[i].Tool == tool && rows[i].Tag == tag && rows[i].Directory == cwd {
			hit = &rows[i]
		}
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

func (s *sidecar) release() {
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
