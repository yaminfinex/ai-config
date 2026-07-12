// Package v2 loads the spec-shaped herder registry projection.
package v2

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

const (
	KindSession   = "session"
	KindNode      = "node"
	KindNamespace = "namespace"
	KindEpoch     = "epoch"

	StateSeated   = "seated"
	StateUnseated = "unseated"
	StateRetired  = "retired"
	StateLost     = "lost"
)

type LoadOptions struct {
	LocalNodeID string
	Stderr      io.Writer
}

type Projection struct {
	sessions    map[string]SessionRecord
	nodes       map[string]NodeRecord
	namespaces  map[string]NamespaceRecord
	epochs      map[string]EpochRecord
	anomalies   []Anomaly
	quarantined []Quarantine
}

type SessionRecord struct {
	Kind        string          `json:"kind,omitempty"`
	GUID        string          `json:"guid"`
	Event       string          `json:"event"`
	RecordedAt  string          `json:"recorded_at"`
	Node        string          `json:"node"`
	State       string          `json:"state"`
	Label       string          `json:"label,omitempty"`
	Role        string          `json:"role,omitempty"`
	Tool        string          `json:"tool,omitempty"`
	Seat        *Seat           `json:"seat,omitempty"`
	SIDs        []SID           `json:"sids,omitempty"`
	Continuity  string          `json:"continuity,omitempty"`
	Lineage     Lineage         `json:"lineage,omitempty"`
	Provenance  Provenance      `json:"provenance,omitempty"`
	CloseResult string          `json:"close_result,omitempty"`
	CloseReason string          `json:"close_reason,omitempty"`
	ObservedVia string          `json:"observed_via,omitempty"`
	Raw         json.RawMessage `json:"-"`
	Ordinal     int             `json:"-"`
	LegacyV1    bool            `json:"-"`
}

type Seat struct {
	Kind         string `json:"kind"`
	Node         string `json:"node"`
	TerminalID   string `json:"terminal_id,omitempty"`
	PaneID       string `json:"pane_id,omitempty"`
	PID          int    `json:"pid,omitempty"`
	HcomName     string `json:"hcom_name,omitempty"`
	HcomVerified *bool  `json:"hcom_verified,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	HcomEpoch    string `json:"hcom_epoch,omitempty"`
	HerdrEpoch   string `json:"herdr_epoch,omitempty"`
	ConfirmedAt  string `json:"confirmed_at,omitempty"`
}

type SID struct {
	SID        string `json:"sid"`
	Scope      string `json:"scope,omitempty"`
	ObservedAt string `json:"observed_at,omitempty"`
	Source     string `json:"source,omitempty"`
}

type Lineage struct {
	ForkedFrom       string `json:"forked_from,omitempty"`
	ClearedFrom      string `json:"cleared_from,omitempty"`
	DisplacedBy      string `json:"displaced_by,omitempty"`
	ResumeFailedFrom string `json:"resume_failed_from,omitempty"`
}

type Provenance struct {
	Mechanism     string `json:"mechanism,omitempty"`
	SpawnedBy     string `json:"spawned_by,omitempty"`
	ToolSessionID string `json:"tool_session_id,omitempty"`
	Tag           string `json:"tag,omitempty"`
	BatchID       string `json:"batch_id,omitempty"`
	CWD           string `json:"cwd,omitempty"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	Branch        string `json:"branch,omitempty"`
	TS            string `json:"ts,omitempty"`
	ForkedFrom    string `json:"forked_from,omitempty"`
	ResumedAt     string `json:"resumed_at,omitempty"`
}

type NodeRecord struct {
	Kind       string          `json:"kind"`
	Event      string          `json:"event"`
	NodeID     string          `json:"node_id"`
	User       string          `json:"user,omitempty"`
	Hostname   string          `json:"hostname,omitempty"`
	RecordedAt string          `json:"recorded_at"`
	Raw        json.RawMessage `json:"-"`
	Ordinal    int             `json:"-"`
}

type NamespaceRecord struct {
	Kind        string          `json:"kind"`
	Event       string          `json:"event"`
	NamespaceID string          `json:"namespace_id"`
	Node        string          `json:"node,omitempty"`
	Path        string          `json:"path,omitempty"`
	RecordedAt  string          `json:"recorded_at"`
	Raw         json.RawMessage `json:"-"`
	Ordinal     int             `json:"-"`
}

type EpochRecord struct {
	Kind        string                     `json:"kind"`
	Event       string                     `json:"event"`
	EpochID     string                     `json:"epoch_id"`
	Substrate   string                     `json:"substrate"`
	Node        string                     `json:"node,omitempty"`
	Namespace   string                     `json:"namespace,omitempty"`
	Fingerprint map[string]json.RawMessage `json:"fingerprint,omitempty"`
	RecordedAt  string                     `json:"recorded_at"`
	Raw         json.RawMessage            `json:"-"`
	Ordinal     int                        `json:"-"`
}

type Anomaly struct {
	Type       string   `json:"type"`
	Message    string   `json:"message"`
	Line       int      `json:"line,omitempty"`
	GUID       string   `json:"guid,omitempty"`
	Label      string   `json:"label,omitempty"`
	Node       string   `json:"node,omitempty"`
	WinnerGUID string   `json:"winner_guid,omitempty"`
	GUIDs      []string `json:"guids,omitempty"`
}

type Quarantine struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
	Raw    string `json:"raw,omitempty"`
}

func LoadFile(path string, opts LoadOptions) (*Projection, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Load(f, opts)
}

func Load(r io.Reader, opts LoadOptions) (*Projection, error) {
	p := &Projection{
		sessions:   map[string]SessionRecord{},
		nodes:      map[string]NodeRecord{},
		namespaces: map[string]NamespaceRecord{},
		epochs:     map[string]EpochRecord{},
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	br := bufio.NewReader(r)
	for lineNo := 1; ; lineNo++ {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			p.ingestLine(bytes.TrimSpace(line), lineNo, opts)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
	}
	p.detectUnknownNodes()
	p.detectLabelConflicts()
	return p, nil
}

func (p *Projection) Sessions() []SessionRecord {
	out := make([]SessionRecord, 0, len(p.sessions))
	for _, rec := range p.sessions {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GUID < out[j].GUID })
	return out
}

func (p *Projection) Nodes() []NodeRecord {
	out := make([]NodeRecord, 0, len(p.nodes))
	for _, rec := range p.nodes {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

func (p *Projection) Namespaces() []NamespaceRecord {
	out := make([]NamespaceRecord, 0, len(p.namespaces))
	for _, rec := range p.namespaces {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NamespaceID < out[j].NamespaceID })
	return out
}

func (p *Projection) Epochs() []EpochRecord {
	out := make([]EpochRecord, 0, len(p.epochs))
	for _, rec := range p.epochs {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EpochID < out[j].EpochID })
	return out
}

func (p *Projection) Anomalies() []Anomaly {
	out := make([]Anomaly, len(p.anomalies))
	copy(out, p.anomalies)
	return out
}

func (p *Projection) Quarantined() []Quarantine {
	out := make([]Quarantine, len(p.quarantined))
	copy(out, p.quarantined)
	return out
}

func (p *Projection) ingestLine(raw []byte, lineNo int, opts LoadOptions) {
	if len(raw) == 0 {
		return
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		p.quarantine(lineNo, err.Error(), raw, opts.Stderr)
		return
	}
	kind := rawString(obj["kind"])
	if kind == "" {
		kind = KindSession
	}
	switch kind {
	case KindSession:
		rec, ok := p.sessionRecord(obj, raw, lineNo, opts.Stderr)
		if !ok {
			return
		}
		if prev, ok := p.sessions[rec.GUID]; ok && prev.State == StateSeated && rec.State == StateSeated && !sameSeat(prev.Seat, rec.Seat) {
			p.anomalies = append(p.anomalies, Anomaly{
				Type:       "double-seated-session",
				Message:    fmt.Sprintf("session %s has seated rows in two seats; later file row wins", rec.GUID),
				Line:       lineNo,
				GUID:       rec.GUID,
				WinnerGUID: rec.GUID,
			})
		}
		p.sessions[rec.GUID] = rec
	case KindNode:
		var rec NodeRecord
		if err := json.Unmarshal(raw, &rec); err != nil || rec.NodeID == "" {
			p.quarantine(lineNo, structuralReason("node", err, "node_id"), raw, opts.Stderr)
			return
		}
		rec.Raw = bytes.Clone(raw)
		rec.Ordinal = lineNo
		p.nodes[rec.NodeID] = rec
	case KindNamespace:
		var rec NamespaceRecord
		if err := json.Unmarshal(raw, &rec); err != nil || rec.NamespaceID == "" {
			p.quarantine(lineNo, structuralReason("namespace", err, "namespace_id"), raw, opts.Stderr)
			return
		}
		rec.Raw = bytes.Clone(raw)
		rec.Ordinal = lineNo
		p.namespaces[rec.NamespaceID] = rec
	case KindEpoch:
		var rec EpochRecord
		if err := json.Unmarshal(raw, &rec); err != nil || rec.EpochID == "" {
			p.quarantine(lineNo, structuralReason("epoch", err, "epoch_id"), raw, opts.Stderr)
			return
		}
		rec.Raw = bytes.Clone(raw)
		rec.Ordinal = lineNo
		p.epochs[rec.EpochID] = rec
	default:
		p.quarantine(lineNo, "unknown kind "+kind, raw, opts.Stderr)
	}
}

func (p *Projection) sessionRecord(obj map[string]json.RawMessage, raw []byte, lineNo int, stderr io.Writer) (SessionRecord, bool) {
	if _, ok := obj["status"]; ok && rawString(obj["state"]) == "" {
		rec, err := legacySession(obj, raw, lineNo)
		if err != nil {
			p.quarantine(lineNo, err.Error(), raw, stderr)
			return SessionRecord{}, false
		}
		return rec, true
	}
	var rec SessionRecord
	if err := json.Unmarshal(raw, &rec); err != nil || rec.GUID == "" {
		p.quarantine(lineNo, structuralReason("session", err, "guid"), raw, stderr)
		return SessionRecord{}, false
	}
	rec.Raw = bytes.Clone(raw)
	rec.Ordinal = lineNo
	if rec.Kind == "" {
		rec.Kind = KindSession
	}
	return rec, true
}

func legacySession(obj map[string]json.RawMessage, raw []byte, lineNo int) (SessionRecord, error) {
	guid := rawString(obj["guid"])
	if guid == "" {
		return SessionRecord{}, fmt.Errorf("legacy session row missing guid")
	}
	status := rawString(obj["status"])
	state := ""
	switch status {
	case "active":
		state = StateUnseated
	case "closed":
		state = StateRetired
	default:
		return SessionRecord{}, fmt.Errorf("legacy session %s has unknown status %q", guid, status)
	}
	var prov Provenance
	_ = json.Unmarshal(obj["provenance"], &prov)
	rec := SessionRecord{
		Kind:       KindSession,
		GUID:       guid,
		Event:      "legacy_v1_mapped",
		RecordedAt: firstNonEmpty(rawString(obj["closed_at"]), prov.TS, rawString(obj["started_at"])),
		State:      state,
		Label:      rawString(obj["label"]),
		Role:       rawString(obj["role"]),
		Tool:       rawString(obj["agent"]),
		Continuity: "assumed",
		Lineage:    Lineage{ForkedFrom: firstNonEmpty(prov.ForkedFrom, rawString(obj["forked_from"]))},
		Provenance: prov,
		Raw:        bytes.Clone(raw),
		Ordinal:    lineNo,
		LegacyV1:   true,
	}
	if prov.ToolSessionID != "" {
		rec.SIDs = []SID{{SID: prov.ToolSessionID, ObservedAt: prov.TS, Source: "harvest"}}
		rec.Continuity = "confirmed"
	}
	return rec, nil
}

func (p *Projection) detectUnknownNodes() {
	for _, rec := range p.sessions {
		if rec.Node == "" || p.hasNode(rec.Node) {
			continue
		}
		p.anomalies = append(p.anomalies, Anomaly{
			Type:    "unknown-node",
			Message: fmt.Sprintf("session row %s attributed to unregistered node %s", rec.GUID, rec.Node),
			Line:    rec.Ordinal,
			GUID:    rec.GUID,
			Node:    rec.Node,
		})
	}
	for _, rec := range p.namespaces {
		if rec.Node == "" || p.hasNode(rec.Node) {
			continue
		}
		p.anomalies = append(p.anomalies, Anomaly{
			Type:    "unknown-node",
			Message: fmt.Sprintf("namespace row %s attributed to unregistered node %s", rec.NamespaceID, rec.Node),
			Line:    rec.Ordinal,
			Node:    rec.Node,
		})
	}
	for _, rec := range p.epochs {
		if rec.Node == "" || p.hasNode(rec.Node) {
			continue
		}
		p.anomalies = append(p.anomalies, Anomaly{
			Type:    "unknown-node",
			Message: fmt.Sprintf("epoch row %s attributed to unregistered node %s", rec.EpochID, rec.Node),
			Line:    rec.Ordinal,
			Node:    rec.Node,
		})
	}
}

func (p *Projection) hasNode(nodeID string) bool {
	_, ok := p.nodes[nodeID]
	return ok
}

func (p *Projection) detectLabelConflicts() {
	byLabel := map[string][]SessionRecord{}
	for _, rec := range p.sessions {
		if rec.Label == "" || rec.State == StateRetired || rec.State == StateLost {
			continue
		}
		byLabel[rec.Label] = append(byLabel[rec.Label], rec)
	}
	labels := make([]string, 0, len(byLabel))
	for label, holders := range byLabel {
		if len(holders) < 2 {
			continue
		}
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		holders := byLabel[label]
		sort.Slice(holders, func(i, j int) bool { return holders[i].Ordinal < holders[j].Ordinal })
		winner := holders[len(holders)-1]
		guids := make([]string, len(holders))
		for i, rec := range holders {
			guids[i] = rec.GUID
		}
		p.anomalies = append(p.anomalies, Anomaly{
			Type:       "duplicate-live-label",
			Message:    fmt.Sprintf("label %q is held by multiple non-retired sessions; later file row wins", label),
			Label:      label,
			WinnerGUID: winner.GUID,
			GUIDs:      guids,
		})
	}
}

func (p *Projection) quarantine(lineNo int, reason string, raw []byte, stderr io.Writer) {
	q := Quarantine{Line: lineNo, Reason: reason, Raw: string(raw)}
	p.quarantined = append(p.quarantined, q)
	fmt.Fprintf(stderr, "herder registry: quarantined line %d: %s\n", lineNo, reason)
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

func sameSeat(a, b *Seat) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Kind == b.Kind &&
		a.Node == b.Node &&
		a.TerminalID == b.TerminalID &&
		a.PaneID == b.PaneID &&
		a.PID == b.PID &&
		a.HcomName == b.HcomName &&
		a.Namespace == b.Namespace &&
		a.HcomEpoch == b.HcomEpoch &&
		a.HerdrEpoch == b.HerdrEpoch
}

func structuralReason(kind string, err error, required string) string {
	if err != nil {
		return fmt.Sprintf("%s row: %v", kind, err)
	}
	return fmt.Sprintf("%s row missing %s", kind, required)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
