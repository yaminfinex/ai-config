// Package registry reads and appends the herder agent registry: an
// append-only JSONL file at $HERDER_STATE_DIR/registry.jsonl (default
// ${XDG_STATE_HOME:-~/.local/state}/herder). Later rows for the same guid
// supersede earlier ones (status updates, cull unseated session records).
//
// Every bash reader collapses the file through one jq idiom —
//
//	group_by(.guid) | map(.[-1])
//
// — whose exact collapse semantics are load-bearing for the list surface:
// output is sorted by guid ascending (jq value order: null before any
// string, strings by codepoint), and the sort is stable so the LAST file row
// of each guid wins. LatestByGUID reproduces that contract exactly.
package registry

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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

	Role                 string                   `json:"role"`
	Agent                string                   `json:"agent"`
	Provider             string                   `json:"provider,omitempty"`
	Model                string                   `json:"model,omitempty"`
	VendorVersion        *v2.VendorVersionHistory `json:"vendor_version,omitempty"`
	PaneID               string                   `json:"pane_id"`
	TerminalID           string                   `json:"terminal_id"`
	PID                  int                      `json:"pid,omitempty"`
	Team                 string                   `json:"team"`
	HcomDir              string                   `json:"hcom_dir"`
	HcomName             string                   `json:"hcom_name"`
	HcomVerified         *bool                    `json:"hcom_verified,omitempty"`
	HooksBound           *bool                    `json:"hooks_bound,omitempty"`
	TranscriptPath       string                   `json:"transcript_path,omitempty"`
	CredentialGeneration string                   `json:"credential_generation,omitempty"`
	HcomTag              string                   `json:"hcom_tag"`
	Status               string                   `json:"status"`
	State                string                   `json:"state,omitempty"`
	RecordedAt           string                   `json:"recorded_at,omitempty"`
	CloseResult          string                   `json:"close_result,omitempty"`
	CloseReason          string                   `json:"close_reason,omitempty"`
	ObservedVia          string                   `json:"observed_via,omitempty"`
	Capabilities         *v2.Capabilities         `json:"capabilities,omitempty"`
	Mission              *v2.Mission              `json:"mission,omitempty"`
	Provenance           *Provenance              `json:"provenance,omitempty"`

	Archived bool            `json:"-"`
	Raw      json.RawMessage `json:"-"`
}

// Provenance records how an identity row entered the registry. It is optional
// so old rows remain valid and raw-list output can continue to pass them
// through without synthetic fields.
type Provenance struct {
	Mechanism              string `json:"mechanism"`
	SpawnedBy              string `json:"spawned_by"`
	ToolSessionID          string `json:"tool_session_id"`
	Tag                    string `json:"tag"`
	BatchID                string `json:"batch_id"`
	CWD                    string `json:"cwd"`
	WorkspaceID            string `json:"workspace_id"`
	Branch                 string `json:"branch"`
	TS                     string `json:"ts"`
	ForkedFrom             string `json:"forked_from,omitempty"`
	ResumedAt              string `json:"resumed_at,omitempty"`
	CredentialNoticeSender string `json:"credential_notice_sender,omitempty"`
	CredentialNoticeBusDir string `json:"credential_notice_bus_dir,omitempty"`
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
	recs, err := loadArchives(path)
	if err != nil {
		return nil, err
	}
	live, err := Load(path)
	if err != nil {
		return nil, err
	}
	return append(recs, live...), nil
}

func loadArchives(path string) ([]Record, error) {
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
		recs, decodeErr := decode(f, archive, true)
		closeErr := f.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		out = append(out, recs...)
	}
	return out, nil
}

func registryArchivePaths(path string) ([]string, error) {
	dir := filepath.Join(filepath.Dir(path), filepath.Base(path)+".archive")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || archiveSequence(entry.Name()) == 0 || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Slice(paths, func(i, j int) bool {
		return archiveSequence(filepath.Base(paths[i])) < archiveSequence(filepath.Base(paths[j]))
	})
	return paths, nil
}

func archiveSequence(name string) int {
	if len(name) < 5 || name[4] != '-' {
		return 0
	}
	sequence, err := strconv.Atoi(name[:4])
	if err != nil || sequence <= 0 {
		return 0
	}
	return sequence
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
	short := shortGUID(guid)
	label := rawString(obj["label"])
	var seat v2.Seat
	_ = json.Unmarshal(obj["seat"], &seat)
	var prov Provenance
	_ = json.Unmarshal(obj["provenance"], &prov)
	rec := Record{
		Role:                 rawString(obj["role"]),
		Agent:                rawString(obj["tool"]),
		Provider:             rawString(obj["provider"]),
		Model:                rawString(obj["model"]),
		PaneID:               seat.PaneID,
		TerminalID:           seat.TerminalID,
		PID:                  seat.PID,
		Team:                 rawString(obj["team"]),
		HcomDir:              seat.Namespace,
		HcomName:             seat.HcomName,
		HcomVerified:         seat.HcomVerified,
		HooksBound:           boolPointer(seat.HooksBound),
		TranscriptPath:       seat.TranscriptPath,
		CredentialGeneration: seat.CredentialGeneration,
		State:                rawString(obj["state"]),
		CloseResult:          rawString(obj["close_result"]),
		CloseReason:          rawString(obj["close_reason"]),
		ObservedVia:          rawString(obj["observed_via"]),
		Provenance:           &prov,
	}
	var vendorVersion v2.VendorVersionHistory
	if json.Unmarshal(obj["vendor_version"], &vendorVersion) == nil && vendorVersion.Current != (v2.VendorVersionObservation{}) {
		rec.VendorVersion = &vendorVersion
	}
	var capabilities v2.Capabilities
	if json.Unmarshal(obj["capabilities"], &capabilities) == nil && capabilities != (v2.Capabilities{}) {
		rec.Capabilities = &capabilities
	}
	var mission v2.Mission
	if json.Unmarshal(obj["mission"], &mission) == nil && mission.Slug != "" {
		rec.Mission = &mission
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

func IsNonRetired(rec Record) bool {
	return rec.State == v2.StateSeated || rec.State == v2.StateUnseated
}

func newGUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func shortGUID(guid string) string {
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

func boolPointer(value bool) *bool {
	if !value {
		return nil
	}
	v := true
	return &v
}

func cloneVendorVersion(history *v2.VendorVersionHistory) *v2.VendorVersionHistory {
	if history == nil {
		return nil
	}
	copy := *history
	if history.Previous != nil {
		previous := *history.Previous
		copy.Previous = &previous
	}
	return &copy
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
