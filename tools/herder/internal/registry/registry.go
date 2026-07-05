// Package registry reads and appends the herder agent registry: an
// append-only JSONL file at $HERDER_STATE_DIR/registry.jsonl (default
// ${XDG_STATE_HOME:-~/.local/state}/herder). Later rows for the same guid
// supersede earlier ones (status updates, cull close records).
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
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
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

	Role       string      `json:"role"`
	Agent      string      `json:"agent"`
	PaneID     string      `json:"pane_id"`
	TerminalID string      `json:"terminal_id"`
	Team       string      `json:"team"`
	HcomDir    string      `json:"hcom_dir"`
	HcomName   string      `json:"hcom_name"`
	HcomTag    string      `json:"hcom_tag"`
	Status     string      `json:"status"`
	Provenance *Provenance `json:"provenance,omitempty"`

	Raw json.RawMessage `json:"-"`
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
// The file is parsed as a stream of JSON values (what `jq -s` sees), so
// blank lines and inter-row whitespace are irrelevant; a malformed or
// non-object value is an error, matching jq aborting the whole pipeline.
func Load(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decode(f, path)
}

func decode(r io.Reader, path string) ([]Record, error) {
	var recs []Record
	dec := json.NewDecoder(r)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return recs, nil
			}
			return nil, fmt.Errorf("registry %s: row %d: %w", path, len(recs)+1, err)
		}
		var rec Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			return nil, fmt.Errorf("registry %s: row %d: %w", path, len(recs)+1, err)
		}
		rec.Raw = bytes.Clone(raw)
		recs = append(recs, rec)
	}
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

// ActiveLabelOwner returns the active latest row that owns label, excluding
// exceptGUID. Label writers use this as the registry-level uniqueness
// invariant for rename, enroll, fork, and sidecar manual identity rows.
func ActiveLabelOwner(recs []Record, label, exceptGUID string) *Record {
	if label == "" {
		return nil
	}
	for _, rec := range LatestByGUID(recs) {
		if strEqual(rec.Label, label) && !strEqual(rec.GUID, exceptGUID) && rec.Status == "active" {
			cp := rec
			return &cp
		}
	}
	return nil
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

// Append writes one raw JSON row to the registry, creating the state dir on
// first use (herder spawn's `mkdir -p $STATE_DIR` + `printf '%s\n' >>`).
// The caller owns the row's serialization; Append only guarantees the
// trailing newline that keeps the file valid JSONL.
func Append(path string, row []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(bytes.TrimRight(row, "\n"), '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
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

// BuildProvenance harvests the ambient lineage and workspace fields common to
// spawn, sidecar, and in-session enroll flows. Explicit arguments win over
// ambient values for fields the caller just resolved.
func BuildProvenance(mechanism, tag, cwd, workspaceID string) Provenance {
	spawnedBy := os.Getenv("HERDER_SPAWNED_BY")
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
