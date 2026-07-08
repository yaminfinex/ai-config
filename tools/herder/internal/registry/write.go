package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

var errNoAppend = errors.New("registry append skipped")

type LockedUpdate struct {
	Projection *v2.Projection
}

type LockedUpdateFunc func(LockedUpdate) ([]v2.SessionRecord, error)

// UpdateLocked is the single registry write path. It holds an exclusive flock
// while it loads the v2 projection, validates the caller's session snapshots,
// appends them, and fsyncs the live file before releasing the lock.
func UpdateLocked(path string, fn LockedUpdateFunc) ([][]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return nil, fmt.Errorf("registry lock unavailable for %s: refusing to write unlocked: %w", path, err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	proj, err := v2.Load(f, v2.LoadOptions{})
	if err != nil {
		return nil, err
	}
	var nodeID string
	var mintedRow []byte
	var migratedRows [][]byte
	var rotationRows [][]byte
	if rotationRecoveryNeeded(path, proj) {
		rotationRows, proj, err = recoverRotationLocked(path, f, proj)
		if err != nil {
			return nil, err
		}
		nodeID, mintedRow, proj, err = ensureLockedNode(path, f, proj)
		if err != nil {
			return nil, err
		}
	} else if migrationNeeded(path, proj) {
		nodeID, mintedRow, err = ensureMigrationNode(path, proj)
		if err != nil {
			return nil, err
		}
		migratedRows, proj, err = migrateLegacyV1Locked(path, f, proj, nodeID, mintedRow)
		if err != nil {
			return nil, err
		}
		mintedRow = nil
	} else {
		nodeID, mintedRow, proj, err = ensureLockedNode(path, f, proj)
		if err != nil {
			return nil, err
		}
	}
	if len(migratedRows) == 0 && len(rotationRows) == 0 {
		rotationRows, proj, err = rotateIfNeededLocked(path, f, proj, nodeID)
		if err != nil {
			return nil, err
		}
		if len(rotationRows) > 0 {
			mintedRow = nil
		}
	}
	rows, err := fn(LockedUpdate{Projection: proj})
	if err != nil {
		return nil, err
	}
	var encoded [][]byte
	if len(mintedRow) > 0 {
		encoded = append(encoded, mintedRow)
	}
	encoded = append(encoded, migratedRows...)
	encoded = append(encoded, rotationRows...)
	for _, row := range rows {
		if current := V2ByGUID(proj, row.GUID); current != nil && !sessionHasRegisteredNode(proj, *current) {
			return nil, fmt.Errorf("registry refused to mutate guid %s: latest row is attributed to unknown node %s (no node_registered row)", current.GUID, current.Node)
		}
		normalized, ok, err := normalizeSessionAppend(proj, row)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if owner := V2LabelOwner(proj, normalized.Label, normalized.GUID); owner != nil && isNonRetired(normalized.State) {
			return nil, fmt.Errorf("label %q already belongs to active guid %s", normalized.Label, owner.GUID)
		}
		normalized, err = stampSessionNode(normalized, nodeID)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(normalized)
		if err != nil {
			return nil, err
		}
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return nil, err
		}
		if _, err := f.Write(append(bytes.TrimRight(b, "\n"), '\n')); err != nil {
			return nil, err
		}
		encoded = append(encoded, b)
		proj, err = projectionWithAppended(proj, b)
		if err != nil {
			return nil, err
		}
	}
	if len(encoded) == 0 {
		return nil, nil
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	return encoded, nil
}

func ensureLockedNode(path string, f *os.File, proj *v2.Projection) (string, []byte, *v2.Projection, error) {
	markerPath := NodeMarkerPath(path)
	marker, markerPresent, err := readNodeMarker(markerPath)
	if err != nil {
		return "", nil, proj, err
	}
	nodes := proj.Nodes()
	if markerPresent && hasNode(nodes, marker) {
		return marker, nil, proj, nil
	}
	if !markerPresent && len(nodes) == 0 {
		return mintLockedNode(path, f, proj, "")
	}
	return "", nil, proj, nodeGateError(marker, markerPresent, nodes)
}

func mintLockedNode(path string, f *os.File, proj *v2.Projection, nodeID string) (string, []byte, *v2.Projection, error) {
	if nodeID == "" {
		var err error
		nodeID, err = NewGUID()
		if err != nil {
			return "", nil, proj, err
		}
	}
	row := v2.NodeRecord{
		Kind:       v2.KindNode,
		Event:      "node_registered",
		NodeID:     nodeID,
		User:       os.Getenv("USER"),
		Hostname:   hostname(),
		RecordedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	b, err := json.Marshal(row)
	if err != nil {
		return "", nil, proj, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return "", nil, proj, err
	}
	if _, err := f.Write(append(bytes.TrimRight(b, "\n"), '\n')); err != nil {
		return "", nil, proj, err
	}
	if err := writeNodeMarker(NodeMarkerPath(path), nodeID); err != nil {
		return "", nil, proj, err
	}
	if err := f.Sync(); err != nil {
		return "", nil, proj, err
	}
	next, err := projectionWithAppended(proj, b)
	if err != nil {
		return "", nil, proj, err
	}
	return nodeID, b, next, nil
}

func stampSessionNode(row v2.SessionRecord, nodeID string) (v2.SessionRecord, error) {
	if nodeID == "" {
		return row, fmt.Errorf("registry node gate failed: empty local node id")
	}
	row.Node = nodeID
	if row.Seat != nil {
		row.Seat.Node = nodeID
	}
	return row, nil
}

func lockFile(f *os.File) error {
	if os.Getenv("HERDER_TEST_FLOCK_REFUSE") == "1" {
		return syscall.ENOLCK
	}
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func projectionWithAppended(prev *v2.Projection, row []byte) (*v2.Projection, error) {
	var buf bytes.Buffer
	for _, rec := range prev.Sessions() {
		if len(rec.Raw) == 0 {
			continue
		}
		buf.Write(rec.Raw)
		buf.WriteByte('\n')
	}
	for _, rec := range prev.Nodes() {
		buf.Write(rec.Raw)
		buf.WriteByte('\n')
	}
	for _, rec := range prev.Namespaces() {
		buf.Write(rec.Raw)
		buf.WriteByte('\n')
	}
	for _, rec := range prev.Epochs() {
		buf.Write(rec.Raw)
		buf.WriteByte('\n')
	}
	buf.Write(row)
	buf.WriteByte('\n')
	return v2.Load(&buf, v2.LoadOptions{})
}

func readNodeMarker(path string) (string, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", false, fmt.Errorf("registry node gate refused: empty node marker %s; run `herder node init` to repair the state dir", path)
	}
	if !isNodeIDShape(id) {
		return "", false, fmt.Errorf("registry node gate refused: malformed node marker %s contains %q; run `herder node init` to repair the state dir", path, id)
	}
	return id, true, nil
}

func readNodeMarkerLenient(path string) (string, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(b)), true, nil
}

func isNodeIDShape(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, r := range id {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

func writeNodeMarker(path, nodeID string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(nodeID + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func hasNode(nodes []v2.NodeRecord, nodeID string) bool {
	for _, node := range nodes {
		if node.NodeID == nodeID {
			return true
		}
	}
	return false
}

func sessionHasRegisteredNode(proj *v2.Projection, rec v2.SessionRecord) bool {
	return rec.Node == "" || hasNode(proj.Nodes(), rec.Node)
}

func nodeGateError(marker string, markerPresent bool, nodes []v2.NodeRecord) error {
	var state string
	switch {
	case markerPresent && len(nodes) == 0:
		state = fmt.Sprintf("marker contains %s but registry has no node_registered row", marker)
	case !markerPresent && len(nodes) > 0:
		state = fmt.Sprintf("registry has node_registered row %s but marker is absent", nodes[0].NodeID)
	case markerPresent && len(nodes) > 0:
		state = fmt.Sprintf("marker contains %s but registry node rows are %s", marker, nodeIDs(nodes))
	default:
		state = "marker and registry node state are inconsistent"
	}
	return fmt.Errorf("registry node gate refused: %s; run `herder node init` to repair the state dir", state)
}

func nodeIDs(nodes []v2.NodeRecord) string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.NodeID)
	}
	return strings.Join(out, ",")
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

func normalizeSessionAppend(proj *v2.Projection, row v2.SessionRecord) (v2.SessionRecord, bool, error) {
	if row.GUID == "" {
		return row, false, fmt.Errorf("session row missing guid")
	}
	if row.RecordedAt == "" {
		row.RecordedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}
	if row.Kind == "" {
		row.Kind = v2.KindSession
	}
	if row.Event == "" {
		row.Event = "seated"
	}
	if row.State == "" {
		if row.Seat != nil {
			row.State = v2.StateSeated
		} else {
			row.State = v2.StateUnseated
		}
	}
	current := V2ByGUID(proj, row.GUID)
	if current == nil {
		return row, true, nil
	}
	switch row.Event {
	case "unseated":
		if current.State == row.State && (current.CloseResult != "" || row.CloseResult == "") && !current.LegacyV1 {
			return row, false, nil
		}
		if current.State == v2.StateRetired || current.State == v2.StateLost {
			return row, false, nil
		}
		row = carryIdentityFields(row, *current)
		row.Seat = nil
	case "retired":
		if current.State == v2.StateRetired || current.State == v2.StateLost {
			return row, false, nil
		}
		row = carryUnlabelledIdentityFields(row, *current)
		row.State = v2.StateRetired
		row.Label = ""
		row.Seat = nil
	case "reopened":
		if current.State != v2.StateRetired {
			return row, false, nil
		}
		row = carryUnlabelledIdentityFields(row, *current)
		row.State = v2.StateUnseated
		row.Label = ""
		row.Seat = nil
	case "labelled", "label_transferred":
		if current.Label == row.Label {
			return row, false, nil
		}
		row = carrySeatFields(row, *current)
	case "recognised", "reconciled", "seated":
		if current.State == v2.StateRetired || current.State == v2.StateLost {
			return row, false, nil
		}
		if (row.Event == "recognised" || row.Event == "reconciled") && current.State == v2.StateUnseated && !current.LegacyV1 {
			return row, false, nil
		}
		if row.Event == "recognised" || row.Event == "reconciled" || row.Label == "" {
			row.Label = current.Label
		}
		row.Role = firstNonEmpty(row.Role, current.Role)
		row.Tool = firstNonEmpty(row.Tool, current.Tool)
		if len(row.SIDs) == 0 {
			row.SIDs = current.SIDs
		}
		if row.Continuity == "" {
			row.Continuity = current.Continuity
		}
		if row.Lineage == (v2.Lineage{}) {
			row.Lineage = current.Lineage
		}
		if row.Provenance == (v2.Provenance{}) {
			row.Provenance = current.Provenance
		}
	case "registered":
		if (current.State == v2.StateRetired || current.State == v2.StateLost) && !current.LegacyV1 {
			return row, false, nil
		}
		row = carryRegisteredFields(row, *current)
		if sameProjectedSession(row, *current) {
			return row, false, nil
		}
	}
	return row, true, nil
}

func carryRegisteredFields(row, current v2.SessionRecord) v2.SessionRecord {
	if row.Seat == nil {
		row.State = current.State
	}
	row.Label = firstNonEmpty(row.Label, current.Label)
	row.Role = firstNonEmpty(row.Role, current.Role)
	row.Tool = firstNonEmpty(row.Tool, current.Tool)
	if len(row.SIDs) == 0 {
		row.SIDs = current.SIDs
	}
	if row.Continuity == "" {
		row.Continuity = current.Continuity
	}
	if row.Lineage == (v2.Lineage{}) {
		row.Lineage = current.Lineage
	}
	if row.Provenance == (v2.Provenance{}) {
		row.Provenance = current.Provenance
	}
	row.Seat = mergeSeatFields(row.Seat, current.Seat)
	return row
}

func mergeSeatFields(patch, current *v2.Seat) *v2.Seat {
	if patch == nil {
		return cloneSeat(current)
	}
	if current == nil {
		return cloneSeat(patch)
	}
	seat := *current
	if patch.Kind != "" {
		seat.Kind = patch.Kind
	}
	if patch.Node != "" {
		seat.Node = patch.Node
	}
	if patch.TerminalID != "" {
		seat.TerminalID = patch.TerminalID
	}
	if patch.PaneID != "" {
		seat.PaneID = patch.PaneID
	}
	if patch.PID != 0 {
		seat.PID = patch.PID
	}
	if patch.HcomName != "" {
		seat.HcomName = patch.HcomName
	}
	if patch.Namespace != "" {
		seat.Namespace = patch.Namespace
	}
	if patch.HcomEpoch != "" {
		seat.HcomEpoch = patch.HcomEpoch
	}
	if patch.HerdrEpoch != "" {
		seat.HerdrEpoch = patch.HerdrEpoch
	}
	if patch.ConfirmedAt != "" {
		seat.ConfirmedAt = patch.ConfirmedAt
	}
	return &seat
}

func cloneSeat(seat *v2.Seat) *v2.Seat {
	if seat == nil {
		return nil
	}
	cp := *seat
	return &cp
}

func carryIdentityFields(row, current v2.SessionRecord) v2.SessionRecord {
	row.Label = current.Label
	return carryUnlabelledIdentityFields(row, current)
}

func carryUnlabelledIdentityFields(row, current v2.SessionRecord) v2.SessionRecord {
	row.Role = firstNonEmpty(row.Role, current.Role)
	row.Tool = firstNonEmpty(row.Tool, current.Tool)
	if len(row.SIDs) == 0 {
		row.SIDs = current.SIDs
	}
	if row.Continuity == "" {
		row.Continuity = current.Continuity
	}
	if row.Lineage == (v2.Lineage{}) {
		row.Lineage = current.Lineage
	}
	if row.Provenance == (v2.Provenance{}) {
		row.Provenance = current.Provenance
	}
	return row
}

func carrySeatFields(row, current v2.SessionRecord) v2.SessionRecord {
	row.Role = firstNonEmpty(row.Role, current.Role)
	row.Tool = firstNonEmpty(row.Tool, current.Tool)
	row.State = current.State
	row.Seat = current.Seat
	if row.Seat == nil && current.LegacyV1 {
		legacy := LegacyFromV2(current)
		if legacy.PaneID != "" || legacy.TerminalID != "" || legacy.HcomName != "" || legacy.HcomDir != "" {
			row.State = v2.StateSeated
			row.Seat = &v2.Seat{
				Kind:        "herdr",
				TerminalID:  legacy.TerminalID,
				PaneID:      legacy.PaneID,
				HcomName:    legacy.HcomName,
				Namespace:   legacy.HcomDir,
				ConfirmedAt: row.RecordedAt,
			}
		}
	}
	if len(row.SIDs) == 0 {
		row.SIDs = current.SIDs
	}
	if row.Continuity == "" {
		row.Continuity = current.Continuity
	}
	if row.Lineage == (v2.Lineage{}) {
		row.Lineage = current.Lineage
	}
	if row.Provenance == (v2.Provenance{}) {
		row.Provenance = current.Provenance
	}
	return row
}

func sameProjectedSession(a, b v2.SessionRecord) bool {
	return a.Kind == b.Kind &&
		a.GUID == b.GUID &&
		a.State == b.State &&
		a.Label == b.Label &&
		a.Role == b.Role &&
		a.Tool == b.Tool &&
		sameSeatFields(a.Seat, b.Seat) &&
		sameSIDs(a.SIDs, b.SIDs) &&
		a.Continuity == b.Continuity &&
		a.Lineage == b.Lineage &&
		a.Provenance == b.Provenance &&
		a.CloseResult == b.CloseResult &&
		a.CloseReason == b.CloseReason
}

func sameSeatFields(a, b *v2.Seat) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Kind == b.Kind &&
		a.TerminalID == b.TerminalID &&
		a.PaneID == b.PaneID &&
		a.PID == b.PID &&
		a.HcomName == b.HcomName &&
		a.Namespace == b.Namespace &&
		a.HcomEpoch == b.HcomEpoch &&
		a.HerdrEpoch == b.HerdrEpoch &&
		a.ConfirmedAt == b.ConfirmedAt
}

func sameSIDs(a, b []v2.SID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func V2ByGUID(proj *v2.Projection, guid string) *v2.SessionRecord {
	for _, rec := range proj.Sessions() {
		if rec.GUID == guid {
			cp := rec
			return &cp
		}
	}
	return nil
}

func V2Resolve(proj *v2.Projection, target string) *v2.SessionRecord {
	var hit *v2.SessionRecord
	for _, rec := range proj.Sessions() {
		paneID := ""
		if rec.Seat != nil {
			paneID = rec.Seat.PaneID
		}
		if rec.GUID == target || ShortGUID(rec.GUID) == target || rec.Label == target || paneID == target {
			cp := rec
			hit = &cp
		}
	}
	return hit
}

func V2LabelOwner(proj *v2.Projection, label, exceptGUID string) *v2.SessionRecord {
	if label == "" {
		return nil
	}
	for _, rec := range proj.Sessions() {
		if rec.Label == label && rec.GUID != exceptGUID && isNonRetired(rec.State) && sessionHasRegisteredNode(proj, rec) {
			cp := rec
			return &cp
		}
	}
	return nil
}

func isNonRetired(state string) bool {
	return state != v2.StateRetired && state != v2.StateLost
}
