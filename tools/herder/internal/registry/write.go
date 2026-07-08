package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	rows, err := fn(LockedUpdate{Projection: proj})
	if err != nil {
		return nil, err
	}
	var encoded [][]byte
	for _, row := range rows {
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
	case "unseated", "retired":
		if current.State == row.State && row.Event == "unseated" && !current.LegacyV1 {
			return row, false, nil
		}
		if current.State == v2.StateRetired || current.State == v2.StateLost {
			return row, false, nil
		}
		row = carryIdentityFields(row, *current)
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
		a.Node == b.Node &&
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
		a.Node == b.Node &&
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
		if rec.GUID == target || ShortGUID(rec.GUID) == target || rec.Label == target {
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
		if rec.Label == label && rec.GUID != exceptGUID && isNonRetired(rec.State) {
			cp := rec
			return &cp
		}
	}
	return nil
}

func isNonRetired(state string) bool {
	return state != v2.StateRetired && state != v2.StateLost
}
