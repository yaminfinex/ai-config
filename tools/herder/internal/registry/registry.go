// Package registry reads and appends the herder agent registry: an
// append-only JSONL file at $HERDER_STATE_DIR/registry.jsonl (default
// ${XDG_STATE_HOME:-~/.local/state}/herder). Later rows for the same guid
// supersede earlier ones (status updates, cull unseated session records).
//
// Every bash reader collapses the file through one jq idiom —
//
//	group_by(.guid) | map(.[-1])
//
// — whose exact semantics are load-bearing for the characterization goldens:
// output is sorted by guid ascending (jq value order: null before any
// string, strings by codepoint), the sort is stable so the LAST file row of
// each guid wins, and target resolution then takes `last // empty` of the
// guid/short_guid/label matches. LatestByGUID and Resolve reproduce that
// contract exactly; treat any divergence from jq as a bug here.
package registry

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

// Record is one registry row. The typed fields are the ones the bash
// substrate reads back (resolution keys, routing coordinates, status); Raw
// preserves the row byte-faithfully for re-emit paths (`herder list --json`
// re-serializes the original object plus reconcile fields, keeping the
// writer's key order — jq object semantics a map round-trip would destroy).
//
// GUID, ShortGUID, and Label are pointers because jq distinguishes a missing
// field (null) from an empty string when sorting and when matching
// `select(.guid==$v ...)`; the other fields are only ever read through
// `// empty`-style fallbacks, where null and "" collapse to the same thing.
type Record struct {
	GUID      *string `json:"guid"`
	ShortGUID *string `json:"short_guid"`
	Label     *string `json:"label"`

	Role         string           `json:"role"`
	Agent        string           `json:"agent"`
	PaneID       string           `json:"pane_id"`
	TerminalID   string           `json:"terminal_id"`
	PID          int              `json:"pid,omitempty"`
	Team         string           `json:"team"`
	HcomDir      string           `json:"hcom_dir"`
	HcomName     string           `json:"hcom_name"`
	HcomVerified *bool            `json:"hcom_verified,omitempty"`
	HcomTag      string           `json:"hcom_tag"`
	Status       string           `json:"status"`
	State        string           `json:"state,omitempty"`
	RecordedAt   string           `json:"recorded_at,omitempty"`
	CloseResult  string           `json:"close_result,omitempty"`
	CloseReason  string           `json:"close_reason,omitempty"`
	ObservedVia  string           `json:"observed_via,omitempty"`
	Capabilities *v2.Capabilities `json:"capabilities,omitempty"`
	Provenance   *Provenance      `json:"provenance,omitempty"`

	Archived bool            `json:"-"`
	Raw      json.RawMessage `json:"-"`
}

// Provenance records how an identity row entered the registry. It is optional
// so old rows remain valid and raw-list output can continue to pass them
// through without synthetic fields.
type Provenance struct {
	Mechanism     string `json:"mechanism"`
	SpawnedBy     string `json:"spawned_by"`
	ToolSessionID string `json:"tool_session_id"`
	Tag           string `json:"tag"`
	BatchID       string `json:"batch_id"`
	CWD           string `json:"cwd"`
	WorkspaceID   string `json:"workspace_id"`
	Branch        string `json:"branch"`
	TS            string `json:"ts"`
	ForkedFrom    string `json:"forked_from,omitempty"`
	ResumedAt     string `json:"resumed_at,omitempty"`
}

// DefaultPath resolves the registry location exactly like the bash scripts:
// ${HERDER_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/herder}/registry.jsonl
// (empty env vars count as unset, matching bash `:-`).
func DefaultPath() string {
	stateDir := os.Getenv("HERDER_STATE_DIR")
	if stateDir == "" {
		xdg := os.Getenv("XDG_STATE_HOME")
		if xdg == "" {
			home, _ := os.UserHomeDir()
			xdg = filepath.Join(home, ".local", "state")
		}
		stateDir = filepath.Join(xdg, "herder")
	}
	return filepath.Join(stateDir, "registry.jsonl")
}

// Load reads every row of the registry at path. A missing file returns
// (nil, fs.ErrNotExist)-wrapped error — callers mirror the bash scripts'
// `[[ -f $REGISTRY ]]` guards and decide what "no registry" means for them.
// Malformed or non-object rows are quarantined with a stderr warning and
// skipped. That intentionally diverges from jq's old whole-file failure mode:
// the v2 registry spec requires one torn append to never disable the CLI.
func Load(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decode(f, path, false)
}

func LoadWithArchives(path string) ([]Record, error) {
	recs, err := LoadArchives(path)
	if err != nil {
		return nil, err
	}
	live, err := Load(path)
	if err != nil {
		return nil, err
	}
	return append(recs, live...), nil
}

func LoadArchives(path string) ([]Record, error) {
	archives, err := registryArchivePaths(path)
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, archive := range archives {
		f, err := os.Open(archive)
		if err != nil {
			return nil, err
		}
		recs, decErr := decode(f, archive, true)
		closeErr := f.Close()
		if decErr != nil {
			return nil, decErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		out = append(out, recs...)
	}
	return out, nil
}

func decode(r io.Reader, path string, archived bool) ([]Record, error) {
	var recs []Record
	br := bufio.NewReader(r)
	for lineNo := 1; ; lineNo++ {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			raw := bytes.TrimSpace(line)
			if len(raw) != 0 {
				var rec Record
				if err := json.Unmarshal(raw, &rec); err != nil {
					warnQuarantined(path, lineNo, err)
				} else {
					var obj map[string]json.RawMessage
					if err := json.Unmarshal(raw, &obj); err != nil {
						warnQuarantined(path, lineNo, err)
					} else {
						kind := rawString(obj["kind"])
						if kind != "" && kind != v2.KindSession {
							continue
						}
						if isV2SessionObject(obj) {
							rec = recordFromV2SessionObject(obj)
						} else {
							rec.State = legacyV1State(rec.Status)
						}
						rec.Archived = archived
						rec.Raw = bytes.Clone(raw)
						recs = append(recs, rec)
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return recs, nil
			}
			return nil, err
		}
	}
}

func isV2SessionObject(obj map[string]json.RawMessage) bool {
	kind := rawString(obj["kind"])
	return (kind == "" || kind == v2.KindSession) && rawString(obj["state"]) != ""
}

func recordFromV2SessionObject(obj map[string]json.RawMessage) Record {
	guid := rawString(obj["guid"])
	short := ShortGUID(guid)
	label := rawString(obj["label"])
	var seat v2.Seat
	_ = json.Unmarshal(obj["seat"], &seat)
	var prov Provenance
	_ = json.Unmarshal(obj["provenance"], &prov)
	rec := Record{
		Role:         rawString(obj["role"]),
		Agent:        rawString(obj["tool"]),
		PaneID:       seat.PaneID,
		TerminalID:   seat.TerminalID,
		PID:          seat.PID,
		Team:         rawString(obj["team"]),
		HcomDir:      seat.Namespace,
		HcomName:     seat.HcomName,
		HcomVerified: seat.HcomVerified,
		State:        rawString(obj["state"]),
		CloseResult:  rawString(obj["close_result"]),
		CloseReason:  rawString(obj["close_reason"]),
		ObservedVia:  rawString(obj["observed_via"]),
		Provenance:   &prov,
	}
	var capabilities v2.Capabilities
	if json.Unmarshal(obj["capabilities"], &capabilities) == nil && capabilities != (v2.Capabilities{}) {
		rec.Capabilities = &capabilities
	}
	if guid != "" {
		rec.GUID = &guid
		rec.ShortGUID = &short
	}
	if label != "" {
		rec.Label = &label
	}
	if prov.Tag != "" {
		rec.HcomTag = prov.Tag
	}
	return rec
}

func legacyV1State(status string) string {
	switch status {
	case "active":
		return v2.StateUnseated
	case "closed":
		return v2.StateRetired
	default:
		return ""
	}
}

// LegacyV1RawCompat contains only fields decoded from a legacy-v1 row's Raw
// payload. It exists for migration compatibility where old seat coordinates
// remain operationally relevant; it never derives a two-state status from a
// v2 session state.
type LegacyV1RawCompat struct {
	PaneID       string
	TerminalID   string
	HcomDir      string
	HcomName     string
	HcomVerified *bool
	V1Status     string
}

func DecodeLegacyV1Raw(rec v2.SessionRecord) (LegacyV1RawCompat, bool) {
	if !rec.LegacyV1 || len(bytes.TrimSpace(rec.Raw)) == 0 {
		return LegacyV1RawCompat{}, false
	}
	var raw struct {
		PaneID       string `json:"pane_id"`
		TerminalID   string `json:"terminal_id"`
		HcomDir      string `json:"hcom_dir"`
		HcomName     string `json:"hcom_name"`
		HcomVerified *bool  `json:"hcom_verified,omitempty"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal(rec.Raw, &raw); err != nil {
		return LegacyV1RawCompat{}, false
	}
	return LegacyV1RawCompat{
		PaneID:       raw.PaneID,
		TerminalID:   raw.TerminalID,
		HcomDir:      raw.HcomDir,
		HcomName:     raw.HcomName,
		HcomVerified: raw.HcomVerified,
		V1Status:     raw.Status,
	}, true
}

func warnQuarantined(path string, lineNo int, err error) {
	fmt.Fprintf(os.Stderr, "herder registry %s: quarantined line %d: %v\n", path, lineNo, err)
}

// LatestByGUID collapses rows to the latest record per guid, reproducing
// `group_by(.guid) | map(.[-1])`: stable-sort by guid (null first, then
// codepoint order), keep the last row of each equal-guid run. The result is
// guid-sorted, NOT file-ordered — herder list's output order depends on this.
func LatestByGUID(recs []Record) []Record {
	sorted := make([]Record, len(recs))
	copy(sorted, recs)
	sort.SliceStable(sorted, func(i, j int) bool {
		return guidLess(sorted[i].GUID, sorted[j].GUID)
	})
	var out []Record
	for i, rec := range sorted {
		if i+1 < len(sorted) && guidEqual(rec.GUID, sorted[i+1].GUID) {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// Resolve maps a user-facing target (guid | short_guid | label) to its
// latest registry record, reproducing the shared bash lookup:
//
//	group_by(.guid) | map(.[-1])
//	| map(select(.guid==$v or .short_guid==$v or .label==$v))
//	| last // empty
//
// It returns nil when nothing matches (term_*/raw pane ids never match a
// registry field, which is what routes them to the herdr-verbatim path).
// `last` on the guid-sorted collapse means ties resolve to the greatest guid.
func Resolve(recs []Record, target string) *Record {
	collapsed := LatestByGUID(recs)
	var hit *Record
	for i := range collapsed {
		rec := &collapsed[i]
		if strEqual(rec.GUID, target) || strEqual(rec.ShortGUID, target) || strEqual(rec.Label, target) {
			hit = rec
		}
	}
	return hit
}

// SeatedByPaneOrTerminal resolves a pane_id/terminal_id to the latest seated
// row holding it. Pane ids are display-only and terminal ids are run-scoped;
// ties resolve last
// in guid order, matching the registry's jq semantics. Used by bus-only send
// to map term_*/pane targets to a registry row (and from there to a bus name)
// now that keystroke delivery at raw coordinates is gone.
func SeatedByPaneOrTerminal(recs []Record, key string) *Record {
	if key == "" {
		return nil
	}
	collapsed := LatestByGUID(recs)
	var hit *Record
	for i := range collapsed {
		rec := &collapsed[i]
		if rec.State == v2.StateSeated && (rec.PaneID == key || rec.TerminalID == key) {
			hit = rec
		}
	}
	return hit
}

// UnseatedByPaneOrTerminal finds a dormant row that still carries a stale
// pane or terminal coordinate. Normal v2 writes clear seats on unseat, but
// migrated v1 rows and externally-authored rows can retain these coordinates;
// callers use this only to explain why seated-only resolution refused them.
func UnseatedByPaneOrTerminal(recs []Record, key string) *Record {
	if key == "" {
		return nil
	}
	collapsed := LatestByGUID(recs)
	var hit *Record
	for i := range collapsed {
		rec := collapsed[i]
		if rec.State == v2.StateUnseated && (rec.PaneID == key || rec.TerminalID == key) {
			cp := rec
			hit = &cp
		}
	}
	return hit
}

// SeatedCandidatesByPaneOrTerminal returns EVERY latest seated row whose
// pane_id or terminal_id equals key, in guid order (the same order
// LatestByGUID yields). Pane ids are display-only and may have stale registry
// claimants, so one coordinate can accumulate several seated sessions (for
// example, a stale manual-enroll identity per prior session). Unlike
// SeatedByPaneOrTerminal, which silently keeps only the last, this exposes the
// full candidate set so a caller can disambiguate by bus liveness and refuse
// to guess when more than one is live (TASK-035).
func SeatedCandidatesByPaneOrTerminal(recs []Record, key string) []Record {
	if key == "" {
		return nil
	}
	collapsed := LatestByGUID(recs)
	var out []Record
	for i := range collapsed {
		rec := collapsed[i]
		if rec.State == v2.StateSeated && (rec.PaneID == key || rec.TerminalID == key) {
			out = append(out, rec)
		}
	}
	return out
}

// PickLiveCandidate resolves a set of pane/terminal candidates to the single
// one currently live, per the caller's isLive predicate. Bus liveness is
// package-specific — `herder send` probes each row's own recorded bus, spawn's
// --notify probes the child's bus — so the probe is injected, keeping this
// helper pure. It returns chosen only when EXACTLY one candidate is live, and
// always returns the full live set so an ambiguous caller can render the
// candidate list and decide its own policy (send hard-refuses; notify
// warn-skips). Callers pass this only the multi-candidate case; a lone
// candidate resolves without a liveness probe (TASK-035).
func PickLiveCandidate(candidates []Record, isLive func(Record) bool) (chosen *Record, live []Record) {
	for i := range candidates {
		if isLive(candidates[i]) {
			live = append(live, candidates[i])
		}
	}
	if len(live) == 1 {
		return &live[0], live
	}
	return nil, live
}

// NonRetiredLabelOwner returns the non-retired latest row that owns label,
// excluding exceptGUID. Label writers use this as the registry-level uniqueness
// invariant for rename, enroll, fork, and sidecar manual identity rows.
func NonRetiredLabelOwner(recs []Record, label, exceptGUID string) *Record {
	if label == "" {
		return nil
	}
	for _, rec := range LatestByGUID(recs) {
		if strEqual(rec.Label, label) && !strEqual(rec.GUID, exceptGUID) && IsNonRetired(rec) {
			cp := rec
			return &cp
		}
	}
	return nil
}

func IsNonRetired(rec Record) bool {
	return rec.State == v2.StateSeated || rec.State == v2.StateUnseated
}

func IsSeated(rec Record) bool {
	return rec.State == v2.StateSeated
}

func IsTerminal(rec Record) bool {
	return rec.State == v2.StateRetired || rec.State == v2.StateLost
}

// ResolveByToolSessionID finds any row carrying provenance.tool_session_id and
// returns the latest row for that guid. It intentionally scans all rows, not
// only LatestByGUID, because later append-only rows can temporarily or
// permanently lose the session id while the older session-bearing row remains
// the durable resume/fork key.
func ResolveByToolSessionID(recs []Record, sessionID string) *Record {
	if sessionID == "" {
		return nil
	}
	var matchedGUID string
	var matched *Record
	for i := range recs {
		rec := &recs[i]
		if rec.Provenance == nil || rec.Provenance.ToolSessionID != sessionID {
			continue
		}
		if rec.GUID != nil && *rec.GUID != "" {
			matchedGUID = *rec.GUID
		}
		matched = rec
	}
	if matchedGUID == "" {
		if matched == nil {
			return nil
		}
		cp := *matched
		return &cp
	}
	for _, rec := range LatestByGUID(recs) {
		if strEqual(rec.GUID, matchedGUID) {
			cp := rec
			return &cp
		}
	}
	cp := *matched
	return &cp
}

// ToolSessionIDForGUID returns the last non-empty session id recorded for a
// guid anywhere in the append-only log.
func ToolSessionIDForGUID(recs []Record, guid string) string {
	if guid == "" {
		return ""
	}
	sessionID := ""
	for _, rec := range recs {
		if rec.GUID == nil || *rec.GUID != guid || rec.Provenance == nil {
			continue
		}
		if rec.Provenance.ToolSessionID != "" {
			sessionID = rec.Provenance.ToolSessionID
		}
	}
	return sessionID
}

// PreserveToolSessionID carries a prior durable session id into a new
// provenance value when the current writer did not just observe one.
func PreserveToolSessionID(prov Provenance, recs []Record, guid string) Provenance {
	if prov.ToolSessionID == "" {
		prov.ToolSessionID = ToolSessionIDForGUID(recs, guid)
	}
	return prov
}

// Append writes one session event through the flocked v2 write path. Callers
// should prefer UpdateLocked when the write decision depends on the current
// projection; Append remains for tests and legacy-shaped seed rows.
func Append(path string, row []byte) error {
	rec, err := recordFromJSON(row)
	if err != nil {
		return err
	}
	state := v2.StateSeated
	event := "registered"
	if rec.Status == "closed" {
		state = v2.StateRetired
		event = "retired"
	}
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{V2FromRecord(rec, event, state, "")}, nil
	})
	if err != nil {
		return err
	}
	outcome, err := SingleOutcome(outcomes)
	if err != nil {
		return err
	}
	return outcome.Err()
}

func AppendLegacySessionEvent(path string, row []byte, event, state string) (WriteOutcome, error) {
	rec, err := recordFromJSON(row)
	if err != nil {
		return WriteOutcome{}, err
	}
	outcomes, err := UpdateLocked(path, func(LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{V2FromRecord(rec, event, state, "")}, nil
	})
	if err != nil {
		return WriteOutcome{}, err
	}
	return SingleOutcome(outcomes)
}

func recordFromJSON(row []byte) (Record, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(row), &obj); err != nil {
		return Record{}, err
	}
	if isV2SessionObject(obj) {
		rec := recordFromV2SessionObject(obj)
		overlayLegacyFields(&rec, obj)
		return rec, nil
	}
	var rec Record
	if err := json.Unmarshal(bytes.TrimSpace(row), &rec); err != nil {
		return Record{}, err
	}
	rec.Raw = bytes.Clone(bytes.TrimSpace(row))
	return rec, nil
}

func overlayLegacyFields(rec *Record, obj map[string]json.RawMessage) {
	if v := rawString(obj["guid"]); v != "" {
		rec.GUID = &v
	}
	if v := rawString(obj["short_guid"]); v != "" {
		rec.ShortGUID = &v
	}
	if v := rawString(obj["label"]); v != "" {
		rec.Label = &v
	}
	if v := rawString(obj["role"]); v != "" {
		rec.Role = v
	}
	if v := rawString(obj["agent"]); v != "" {
		rec.Agent = v
	}
	if v := rawString(obj["tool"]); v != "" && rec.Agent == "" {
		rec.Agent = v
	}
	if v := rawString(obj["pane_id"]); v != "" {
		rec.PaneID = v
	}
	if v := rawString(obj["terminal_id"]); v != "" {
		rec.TerminalID = v
	}
	if raw, ok := obj["pid"]; ok {
		_ = json.Unmarshal(raw, &rec.PID)
	}
	if v := rawString(obj["hcom_dir"]); v != "" {
		rec.HcomDir = v
	}
	if v := rawString(obj["hcom_name"]); v != "" {
		rec.HcomName = v
	}
	if raw, ok := obj["hcom_verified"]; ok {
		var verified bool
		if json.Unmarshal(raw, &verified) == nil {
			rec.HcomVerified = &verified
		}
	}
	if v := rawString(obj["hcom_tag"]); v != "" {
		rec.HcomTag = v
	}
	if v := rawString(obj["status"]); v != "" {
		rec.Status = v
	}
	if v := rawString(obj["recorded_at"]); v != "" {
		rec.RecordedAt = v
	}
	if v := rawString(obj["close_result"]); v != "" {
		rec.CloseResult = v
	}
	if v := rawString(obj["close_reason"]); v != "" {
		rec.CloseReason = v
	}
	var prov Provenance
	if err := json.Unmarshal(obj["provenance"], &prov); err == nil && prov != (Provenance{}) {
		rec.Provenance = &prov
	}
	var capabilities v2.Capabilities
	if err := json.Unmarshal(obj["capabilities"], &capabilities); err == nil && capabilities != (v2.Capabilities{}) {
		rec.Capabilities = &capabilities
	}
}

func NewGUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func ShortGUID(guid string) string {
	for i, r := range guid {
		if r == '-' {
			return guid[:i]
		}
	}
	if len(guid) > 8 {
		return guid[:8]
	}
	return guid
}

// BuildProvenance harvests the lineage and workspace fields common to spawn,
// fork, resume, sidecar, and in-session enroll flows. Explicit arguments win
// over ambient values for fields the caller just resolved.
//
// spawnedBy is recorded verbatim when non-empty. Creator flows (spawn, fork,
// resume's no-prior-provenance fallback) pass the session that PERFORMED the
// action ($HERDER_GUID, else "user") so the row agrees with the
// HERDER_SPAWNED_BY they export into the child — the ambient
// $HERDER_SPAWNED_BY in a spawned session names that session's OWN spawner,
// i.e. the child's grandparent. Passing "" harvests the ambient chain
// ($HERDER_SPAWNED_BY, then $HERDER_GUID, then "user"), which is right only
// for rows that describe the CURRENT session (enroll, sidecar): there the
// ambient spawner genuinely is the row's spawner.
func BuildProvenance(mechanism, spawnedBy, tag, cwd, workspaceID string) Provenance {
	if spawnedBy == "" {
		spawnedBy = os.Getenv("HERDER_SPAWNED_BY")
	}
	if spawnedBy == "" {
		spawnedBy = os.Getenv("HERDER_GUID")
	}
	if spawnedBy == "" {
		spawnedBy = "user"
	}
	if tag == "" {
		tag = os.Getenv("HCOM_TAG")
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return Provenance{
		Mechanism:     mechanism,
		SpawnedBy:     spawnedBy,
		ToolSessionID: os.Getenv("HCOM_SESSION_ID"),
		Tag:           tag,
		BatchID:       os.Getenv("HCOM_LAUNCH_BATCH_ID"),
		CWD:           cwd,
		WorkspaceID:   workspaceID,
		Branch:        branchName(cwd),
		TS:            time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// UpdateRawObject returns a full JSON object row containing every field from
// raw plus the supplied replacements. It is used for append-only enrich/rename
// rows where latest-wins replaces entire rows rather than merging them.
func UpdateRawObject(raw []byte, updates map[string]any) ([]byte, error) {
	obj := make(map[string]json.RawMessage)
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, err
		}
	}
	for key, value := range updates {
		b, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		obj[key] = b
	}
	return json.Marshal(obj)
}

func DropRawFields(raw []byte, fields ...string) []byte {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw
	}
	obj := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	for _, field := range fields {
		delete(obj, field)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}

func V2FromRecord(rec Record, event, state, recordedAt string) v2.SessionRecord {
	guid := ptrValue(rec.GUID)
	if state == "" {
		switch rec.Status {
		case "closed":
			state = v2.StateRetired
		case "active":
			state = v2.StateSeated
		default:
			state = v2.StateUnseated
		}
	}
	if recordedAt == "" {
		recordedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}
	prov := v2.Provenance{}
	if rec.Provenance != nil {
		prov = v2.Provenance{
			Mechanism:     rec.Provenance.Mechanism,
			SpawnedBy:     rec.Provenance.SpawnedBy,
			ToolSessionID: rec.Provenance.ToolSessionID,
			Tag:           rec.Provenance.Tag,
			BatchID:       rec.Provenance.BatchID,
			CWD:           rec.Provenance.CWD,
			WorkspaceID:   rec.Provenance.WorkspaceID,
			Branch:        rec.Provenance.Branch,
			TS:            rec.Provenance.TS,
			ForkedFrom:    rec.Provenance.ForkedFrom,
			ResumedAt:     rec.Provenance.ResumedAt,
		}
	}
	out := v2.SessionRecord{
		Kind:        v2.KindSession,
		GUID:        guid,
		Event:       event,
		RecordedAt:  recordedAt,
		State:       state,
		Label:       ptrValue(rec.Label),
		Role:        rec.Role,
		Tool:        rec.Agent,
		Continuity:  "assumed",
		Lineage:     v2.Lineage{ForkedFrom: firstNonEmpty(prov.ForkedFrom)},
		Provenance:  prov,
		CloseResult: rec.CloseResult,
		CloseReason: rec.CloseReason,
		ObservedVia: rec.ObservedVia,
	}
	if rec.Capabilities != nil {
		capabilities := *rec.Capabilities
		out.Capabilities = &capabilities
	}
	if prov.ToolSessionID != "" {
		out.SIDs = []v2.SID{{SID: prov.ToolSessionID, ObservedAt: firstNonEmpty(prov.TS, recordedAt), Source: "harvest"}}
		out.Continuity = "confirmed"
	}
	if state == v2.StateSeated {
		out.Seat = &v2.Seat{
			Kind:         "herdr",
			TerminalID:   rec.TerminalID,
			PaneID:       rec.PaneID,
			PID:          rec.PID,
			HcomName:     rec.HcomName,
			HcomVerified: rec.HcomVerified,
			Namespace:    rec.HcomDir,
			ConfirmedAt:  recordedAt,
		}
	}
	return out
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func ptrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func branchName(cwd string) string {
	if cwd == "" {
		return ""
	}
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(out))
}

// guidLess orders guids the way jq sorts them: null before any string,
// strings by unicode codepoint (byte order for valid UTF-8).
func guidLess(a, b *string) bool {
	if a == nil {
		return b != nil
	}
	if b == nil {
		return false
	}
	return *a < *b
}

func guidEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// strEqual is jq `.field == $v` for a string $v: a missing field is null and
// never equal to a string (even the empty one), so nil never matches.
func strEqual(field *string, v string) bool {
	return field != nil && *field == v
}
