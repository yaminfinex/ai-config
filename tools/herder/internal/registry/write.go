package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/missioncontext"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

var errNoAppend = errors.New("registry append skipped")

type LockedUpdate struct {
	Projection *v2.Projection
	WasMinted  bool
	NodeID     string
}

type LockedUpdateFunc func(LockedUpdate) ([]v2.SessionRecord, error)

type WriteStatus string

const (
	WriteApplied WriteStatus = "applied"
	WriteNoop    WriteStatus = "noop"
	WriteRefused WriteStatus = "refused"
)

// WriteOutcome confirms what happened to one candidate returned by a
// LockedUpdateFunc. Outcomes are positional: outcome i describes candidate i.
// Applied outcomes carry the exact encoded session row appended to the registry;
// refused outcomes carry a reason. Batch errors are reserved for failures where
// the writer cannot confirm an outcome.
type WriteOutcome struct {
	Status WriteStatus
	Row    []byte
	Reason string
	cause  error
}

func (o WriteOutcome) Err() error {
	if o.Status != WriteRefused {
		return nil
	}
	if o.Reason == "" {
		return errors.New("registry write refused without a reason")
	}
	if o.cause != nil {
		return o.cause
	}
	return errors.New(o.Reason)
}

func SingleOutcome(outcomes []WriteOutcome) (WriteOutcome, error) {
	if len(outcomes) != 1 {
		return WriteOutcome{}, fmt.Errorf("registry write returned %d outcomes for one candidate", len(outcomes))
	}
	return outcomes[0], nil
}

type LegacyV1AppendError struct {
	GUID        string
	ArchivePath string
}

func (e *LegacyV1AppendError) Error() string {
	target := "session row"
	if e.GUID != "" {
		target = "session row for guid " + e.GUID
	}
	if e.ArchivePath != "" {
		return fmt.Sprintf("registry refused migration archive %s: it contains a v1-shaped %s alongside v2 node state, so it cannot verify a prior v1 migration; back up the registry and archive, restore the archive from a verified pre-migration backup, identify and excise post-mint v1-shaped rows from the live registry, then retry with the verified archive in place", e.ArchivePath, target)
	}
	return "registry refused v1-shaped append to a minted v2 registry: " + target + " looks like it came from a registry-writing herder binary older than this registry schema; use the spawner HERDER_BIN or upgrade the checkout for new writes. If this fired while mutating an existing poisoned guid, back up the registry, identify and excise the on-disk v1-shaped row, then retry with the verified archive in place"
}

// UpdateLocked is the single registry write path. It holds an exclusive flock
// while it loads the v2 projection, validates the caller's session snapshots,
// appends them, and fsyncs the live file before releasing the lock.
func UpdateLocked(path string, fn LockedUpdateFunc) ([]WriteOutcome, error) {
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
	wasMinted := len(proj.Nodes()) > 0
	if err := refuseFirstV1MigrationForBornV2(path, f, proj); err != nil {
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
	rows, err := fn(LockedUpdate{Projection: proj, WasMinted: wasMinted, NodeID: nodeID})
	if err != nil {
		return nil, err
	}
	registryChanged := len(mintedRow) > 0 || len(migratedRows) > 0 || len(rotationRows) > 0
	outcomes := make([]WriteOutcome, 0, len(rows))
	hasApplied := false
	for i, row := range rows {
		if wasMinted && isLegacyV1SessionAppend(row) {
			return refusedBatch(outcomes, len(rows), i, &LegacyV1AppendError{GUID: row.GUID}), nil
		}
		if current := V2ByGUID(proj, row.GUID); current != nil && !sessionHasRegisteredNode(proj, *current) {
			reason := fmt.Errorf("registry refused to mutate guid %s: latest row is attributed to unknown node %s (no node_registered row)", current.GUID, current.Node)
			return refusedBatch(outcomes, len(rows), i, reason), nil
		}
		normalized, ok, err := normalizeSessionAppend(proj, row)
		if err != nil {
			return refusedBatch(outcomes, len(rows), i, err), nil
		}
		if !ok {
			outcomes = append(outcomes, WriteOutcome{Status: WriteNoop})
			continue
		}
		if owner := V2LabelOwner(proj, normalized.Label, normalized.GUID); owner != nil && isNonRetired(normalized.State) {
			reason := fmt.Errorf("label %q already belongs to non-retired session %s", normalized.Label, owner.GUID)
			return refusedBatch(outcomes, len(rows), i, reason), nil
		}
		normalized, err = stampSessionNode(normalized, nodeID)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(normalized)
		if err != nil {
			return nil, err
		}
		proj, err = projectionWithAppended(proj, b)
		if err != nil {
			return nil, err
		}
		outcomes = append(outcomes, WriteOutcome{Status: WriteApplied, Row: b})
		hasApplied = true
	}
	if hasApplied {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return nil, err
		}
		for _, outcome := range outcomes {
			if outcome.Status != WriteApplied {
				continue
			}
			if _, err := f.Write(append(bytes.TrimRight(outcome.Row, "\n"), '\n')); err != nil {
				return nil, err
			}
			registryChanged = true
		}
	}
	if !registryChanged {
		return outcomes, nil
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	return outcomes, nil
}

func refusedBatch(prior []WriteOutcome, candidateCount, refusedAt int, cause error) []WriteOutcome {
	outcomes := make([]WriteOutcome, candidateCount)
	copy(outcomes, prior)
	for i := range outcomes {
		var reason string
		switch {
		case outcomes[i].Status == WriteApplied:
			reason = fmt.Sprintf("batch refused atomically because candidate %d was refused: %v", refusedAt+1, cause)
		case i == refusedAt:
			reason = cause.Error()
		case i > refusedAt:
			reason = fmt.Sprintf("candidate was not evaluated because candidate %d refused the atomic batch: %v", refusedAt+1, cause)
		default:
			continue
		}
		var outcomeCause error = errors.New(reason)
		if i == refusedAt {
			outcomeCause = cause
		}
		outcomes[i] = WriteOutcome{Status: WriteRefused, Reason: reason, cause: outcomeCause}
	}
	return outcomes
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
	if row.Capabilities != nil {
		switch row.Capabilities.Bus {
		case "", "bound":
		default:
			return row, false, fmt.Errorf("session %s has invalid bus capability %q", row.GUID, row.Capabilities.Bus)
		}
		switch row.Capabilities.Wake {
		case "", "armed", "degraded", "down":
		default:
			return row, false, fmt.Errorf("session %s has invalid wake capability %q", row.GUID, row.Capabilities.Wake)
		}
		if row.Capabilities.Pending < 0 || row.Capabilities.BinderPID < 0 || row.Capabilities.Undeliverable < 0 {
			return row, false, fmt.Errorf("session %s has negative bridge capability counts", row.GUID)
		}
	}
	if err := validateDurableMission(row); err != nil {
		return row, false, err
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
		if row.Lineage.ClearedFrom != "" && row.Mission == nil {
			if displaced := V2ByGUID(proj, row.Lineage.ClearedFrom); displaced != nil {
				row.Mission = cloneMission(displaced.Mission)
			}
		}
		if err := validateDurableMission(row); err != nil {
			return row, false, err
		}
		if err := validateBindingHistory(nil, row.Bindings); err != nil {
			return row, false, err
		}
		attestations, err := normalizeAttestationHistory(nil, row.Attestations)
		if err != nil {
			return row, false, err
		}
		row.Attestations = attestations
		tombstones, err := normalizeTombstoneHistory(nil, row.BindingTombstones, row.Bindings, row.Attestations)
		if err != nil {
			return row, false, err
		}
		row.BindingTombstones = tombstones
		if err := validateAttestedEventOwnership(row, 0, 0); err != nil {
			return row, false, err
		}
		if err := validateSeatedBindingTransition(nil, row); err != nil {
			return row, false, err
		}
		return row, true, nil
	}
	bindings, err := normalizeBindingHistory(current.Bindings, row.Bindings)
	if err != nil {
		return row, false, err
	}
	row.Bindings = bindings
	attestations, err := normalizeAttestationHistory(current.Attestations, row.Attestations)
	if err != nil {
		return row, false, err
	}
	row.Attestations = attestations
	tombstones, err := normalizeTombstoneHistory(current.BindingTombstones, row.BindingTombstones, row.Bindings, row.Attestations)
	if err != nil {
		return row, false, err
	}
	row.BindingTombstones = tombstones
	if err := validateAttestedEventOwnership(row, len(current.Attestations), len(current.BindingTombstones)); err != nil {
		return row, false, err
	}
	if !missionChangeEvent(row) && row.Mission != nil && !sameMission(row.Mission, current.Mission) {
		return row, false, fmt.Errorf("session %s event %q cannot change explicit mission membership", row.GUID, row.Event)
	}
	if clearsMission(row) {
		row.Mission = nil
	} else if row.Mission == nil {
		row.Mission = cloneMission(current.Mission)
	}
	if err := validateDurableMission(row); err != nil {
		return row, false, err
	}
	switch row.Event {
	case "unseated":
		capabilitiesUnchanged := row.Capabilities == nil || sameCapabilities(row.Capabilities, current.Capabilities)
		if current.State == row.State && (current.CloseResult != "" || row.CloseResult == "") && capabilitiesUnchanged && !current.LegacyV1 {
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
	case "adoption_source_released":
		if current.State != v2.StateSeated {
			return row, false, nil
		}
		if row.CloseResult != "adopted" {
			return row, false, fmt.Errorf("adoption source release missing adopted result")
		}
		switch row.CloseReason {
		case "seat superseded by replacement process in the same pane", "operator confirmed old transcript dead":
		default:
			return row, false, fmt.Errorf("adoption source release has unrecognized reason %q", row.CloseReason)
		}
		row = carryUnlabelledIdentityFields(row, *current)
		row.State = v2.StateUnseated
		row.Label = ""
		row.Seat = nil
	case "mission_joined":
		if current.State != v2.StateSeated || row.Mission == nil {
			return row, false, fmt.Errorf("mission join requires a seated session and explicit membership")
		}
		if sameMission(row.Mission, current.Mission) {
			return row, false, nil
		}
		row = carrySeatFields(row, *current)
	case "mission_left":
		if current.State != v2.StateSeated {
			return row, false, fmt.Errorf("mission leave requires a seated session")
		}
		if current.Mission == nil {
			return row, false, nil
		}
		row = carrySeatFields(row, *current)
	case "recognised", "reconciled", "seated", v2.EventAttestedBinding:
		if current.State == v2.StateRetired || current.State == v2.StateLost {
			return row, false, nil
		}
		if (row.Event == "recognised" || row.Event == "reconciled") && current.State == v2.StateUnseated && !current.LegacyV1 {
			return row, false, nil
		}
		if row.Event == "recognised" || row.Event == "reconciled" || row.Event == v2.EventAttestedBinding || row.Label == "" {
			row.Label = current.Label
		}
		if row.Event == v2.EventAttestedBinding {
			row.Role = current.Role
			row.Lineage = current.Lineage
		} else {
			row.Role = firstNonEmpty(row.Role, current.Role)
		}
		row.Tool = firstNonEmpty(row.Tool, current.Tool)
		row = carryPiFacts(row, *current)
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
		row.Capabilities = carryCapabilities(row.Capabilities, current.Capabilities)
	case "registered":
		if (current.State == v2.StateRetired || current.State == v2.StateLost) && !current.LegacyV1 {
			return row, false, nil
		}
		row = carryRegisteredFields(row, *current)
		if sameProjectedSession(row, *current) {
			return row, false, nil
		}
	}
	if err := validateSeatedBindingTransition(current, row); err != nil {
		return row, false, err
	}
	return row, true, nil
}

func validateAttestedEventOwnership(row v2.SessionRecord, priorAttestations, priorTombstones int) error {
	newAttestations := len(row.Attestations) - priorAttestations
	newTombstones := len(row.BindingTombstones) - priorTombstones
	if row.Event != v2.EventAttestedBinding {
		if newAttestations != 0 || newTombstones != 0 {
			return fmt.Errorf("event %q cannot append attestation or binding tombstone history", row.Event)
		}
		return nil
	}
	if newAttestations != 1 {
		return fmt.Errorf("attested_binding event must append exactly one attestation")
	}
	attestation := row.Attestations[len(row.Attestations)-1]
	if attestation.GUID != row.GUID {
		return fmt.Errorf("attestation %q names guid %s, not row guid %s", attestation.ID, attestation.GUID, row.GUID)
	}
	for _, marker := range row.BindingTombstones[priorTombstones:] {
		if marker.AttestationID != attestation.ID {
			return fmt.Errorf("attested_binding event tombstone does not belong to its appended attestation")
		}
	}
	for i := range row.Bindings {
		fact := row.Bindings[i]
		if fact.AttestationID == attestation.ID && fact.EvidenceClass != v2.EvidenceAttested {
			return fmt.Errorf("binding %q names attestation but is not attested evidence", fact.ID)
		}
	}
	if attestation.Operation == v2.AttestationRebind && (attestation.Field == v2.BindingFieldHcomName || attestation.Field == v2.BindingFieldSID) {
		matches := 0
		for _, fact := range row.Bindings {
			if fact.AttestationID == attestation.ID && fact.Field == attestation.Field && fact.Value == attestation.Value && fact.EvidenceClass == v2.EvidenceAttested {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("attested rebind must append exactly one matching correction binding")
		}
	}
	return nil
}

func normalizeAttestationHistory(current, patch []v2.Attestation) ([]v2.Attestation, error) {
	if len(patch) == 0 {
		return cloneAttestations(current), nil
	}
	if len(patch) < len(current) {
		return nil, fmt.Errorf("attestation history is append-only")
	}
	for i := range current {
		if !reflect.DeepEqual(current[i], patch[i]) {
			return nil, fmt.Errorf("attestation history is append-only: persisted attestation %q changed", current[i].ID)
		}
	}
	ids := map[string]bool{}
	for _, attestation := range current {
		ids[attestation.ID] = true
	}
	for _, attestation := range patch[len(current):] {
		if attestation.ID == "" || ids[attestation.ID] {
			return nil, fmt.Errorf("attestation requires a unique durable id")
		}
		ids[attestation.ID] = true
		if attestation.GUID == "" || attestation.Statement == "" || attestation.PaneID == "" || attestation.ObservedAt == "" {
			return nil, fmt.Errorf("attestation %q is incomplete", attestation.ID)
		}
		switch attestation.Operation {
		case v2.AttestationRebind:
			if (attestation.Field != v2.BindingFieldHcomName && attestation.Field != v2.BindingFieldSID && attestation.Field != v2.BindingFieldLaunchContext) || attestation.Value == "" {
				return nil, fmt.Errorf("rebind attestation %q has invalid field or value", attestation.ID)
			}
		case v2.AttestationAuthorizeRecreate:
			if attestation.Field != v2.BindingFieldLaunchContext || attestation.Value == "" {
				return nil, fmt.Errorf("recreate attestation %q must name launch_context and pane value", attestation.ID)
			}
		case v2.AttestationReissueCredential:
			if attestation.Field != "" || attestation.Value != "" {
				return nil, fmt.Errorf("credential reissue attestation %q cannot rebind a field", attestation.ID)
			}
		default:
			return nil, fmt.Errorf("attestation %q has invalid operation %q", attestation.ID, attestation.Operation)
		}
	}
	return cloneAttestations(patch), nil
}

func normalizeTombstoneHistory(current, patch []v2.BindingTombstone, bindings []v2.BindingFact, attestations []v2.Attestation) ([]v2.BindingTombstone, error) {
	if len(patch) == 0 {
		return cloneTombstones(current), nil
	}
	if len(patch) < len(current) {
		return nil, fmt.Errorf("binding tombstone history is append-only")
	}
	for i := range current {
		if !reflect.DeepEqual(current[i], patch[i]) {
			return nil, fmt.Errorf("binding tombstone history is append-only: marker for %q changed", current[i].BindingID)
		}
	}
	byID := make(map[string]v2.BindingFact, len(bindings))
	for _, binding := range bindings {
		byID[binding.ID] = binding
	}
	attestationIDs := make(map[string]bool, len(attestations))
	for _, attestation := range attestations {
		attestationIDs[attestation.ID] = true
	}
	tombstoned := map[string]bool{}
	for _, marker := range current {
		tombstoned[marker.BindingID] = true
	}
	for _, marker := range patch[len(current):] {
		if marker.BindingID == "" {
			return nil, fmt.Errorf("binding tombstone must name one specific durable binding id")
		}
		if tombstoned[marker.BindingID] {
			return nil, fmt.Errorf("binding %q is already tombstoned", marker.BindingID)
		}
		target, ok := byID[marker.BindingID]
		if !ok {
			return nil, fmt.Errorf("binding tombstone target %q does not exist", marker.BindingID)
		}
		correction, ok := byID[marker.CorrectionBindingID]
		if !ok || correction.EvidenceClass != v2.EvidenceAttested || correction.AttestationID == "" {
			return nil, fmt.Errorf("binding tombstone correction %q is not an attested binding", marker.CorrectionBindingID)
		}
		if marker.Field == "" || target.Field != marker.Field || correction.Field != marker.Field {
			return nil, fmt.Errorf("binding tombstone target and correction must have the same field")
		}
		if marker.AttestationID == "" || marker.AttestationID != correction.AttestationID || !attestationIDs[marker.AttestationID] {
			return nil, fmt.Errorf("binding tombstone must name its correction attestation")
		}
		if marker.TombstonedAt == "" {
			return nil, fmt.Errorf("binding tombstone for %q missing tombstoned_at", marker.BindingID)
		}
		tombstoned[marker.BindingID] = true
	}
	return cloneTombstones(patch), nil
}

func normalizeBindingHistory(current, patch []v2.BindingFact) ([]v2.BindingFact, error) {
	if err := validateBindingHistory(nil, current); err != nil {
		return nil, fmt.Errorf("persisted binding history is invalid: %w", err)
	}
	if len(patch) == 0 {
		return cloneBindings(current), nil
	}
	if len(patch) < len(current) {
		return nil, fmt.Errorf("binding history is append-only: patch has %d facts, current row has %d", len(patch), len(current))
	}
	for i := range current {
		if !reflect.DeepEqual(current[i], patch[i]) {
			return nil, fmt.Errorf("binding history is append-only: persisted fact %q was changed or reordered", current[i].ID)
		}
	}
	if err := validateBindingHistory(current, patch[len(current):]); err != nil {
		return nil, err
	}
	return cloneBindings(patch), nil
}

func validateBindingHistory(current, appended []v2.BindingFact) error {
	ids := make(map[string]bool, len(current)+len(appended))
	for _, fact := range current {
		if fact.ID != "" {
			ids[fact.ID] = true
		}
	}
	for _, fact := range appended {
		if fact.ID == "" {
			return fmt.Errorf("binding fact missing durable id")
		}
		if ids[fact.ID] {
			return fmt.Errorf("binding fact id %q is not unique", fact.ID)
		}
		ids[fact.ID] = true
		if fact.ObservedAt == "" {
			return fmt.Errorf("binding fact %q missing observed_at", fact.ID)
		}
		switch fact.EvidenceClass {
		case v2.EvidenceLiveVerified, v2.EvidenceAttested, v2.EvidenceHarvest, v2.EvidenceCarried, v2.EvidenceAssumed:
		default:
			return fmt.Errorf("binding fact %q has invalid evidence class %q", fact.ID, fact.EvidenceClass)
		}
		switch fact.Field {
		case v2.BindingFieldSeat:
			if fact.Seat == nil || fact.Value != "" {
				return fmt.Errorf("seat binding fact %q must carry only a typed seat value", fact.ID)
			}
			if err := validateBindingSeat(*fact.Seat); err != nil {
				return fmt.Errorf("seat binding fact %q: %w", fact.ID, err)
			}
		case v2.BindingFieldHcomName, v2.BindingFieldSID:
			if fact.Value == "" || fact.Seat != nil {
				return fmt.Errorf("binding fact %q for field %s must carry only a nonempty value", fact.ID, fact.Field)
			}
		default:
			return fmt.Errorf("binding fact %q has invalid field %q", fact.ID, fact.Field)
		}
	}
	return nil
}

func validateBindingSeat(seat v2.BindingSeat) error {
	switch seat.Kind {
	case "herdr":
		if seat.TerminalID == "" || seat.PaneID == "" || seat.PID != 0 {
			return fmt.Errorf("herdr seat requires terminal_id + pane_id and no pid")
		}
	case "process":
		if seat.PID <= 0 || seat.TerminalID != "" || seat.PaneID != "" {
			return fmt.Errorf("process seat requires pid and no terminal/pane")
		}
	default:
		return fmt.Errorf("invalid kind %q", seat.Kind)
	}
	return nil
}

func validateSeatedBindingTransition(current *v2.SessionRecord, row v2.SessionRecord) error {
	if row.State != v2.StateSeated {
		return nil
	}
	// Rows written before binding history existed remain readable and mutable so
	// the first completion can upgrade them. Once a row carries binding facts,
	// every seated successor is held to the canonical binding contract.
	if len(row.Bindings) == 0 && (current == nil || len(current.Bindings) == 0) {
		return nil
	}
	if row.Seat == nil {
		return fmt.Errorf("seated session %s is missing a canonical seat", row.GUID)
	}
	if err := validateCurrentSeat(*row.Seat); err != nil {
		return fmt.Errorf("seated session %s: %w", row.GUID, err)
	}
	priorCount := 0
	var priorSeat *v2.Seat
	if current != nil {
		priorCount = len(current.Bindings)
		if current.State == v2.StateSeated {
			priorSeat = current.Seat
		}
	}
	if priorCount > len(row.Bindings) {
		return fmt.Errorf("seated session %s lost binding history", row.GUID)
	}
	appended := row.Bindings[priorCount:]
	if !sameSeatBindingProjection(priorSeat, row.Seat) && !hasMatchingSeatBinding(appended, row.Seat) {
		return fmt.Errorf("seated session %s changes current seat coordinates without a matching binding fact", row.GUID)
	}
	priorName, priorVerified := busBindingProjection(priorSeat)
	name, verified := busBindingProjection(row.Seat)
	if name != priorName || verified != priorVerified {
		if name == "" || !verified || !hasMatchingBusBinding(appended, name) {
			return fmt.Errorf("seated session %s changes current bus binding without a matching binding fact", row.GUID)
		}
	}
	return nil
}

func validateCurrentSeat(seat v2.Seat) error {
	switch seat.Kind {
	case "herdr":
		if seat.TerminalID == "" || seat.PaneID == "" || seat.PID != 0 {
			return fmt.Errorf("herdr seat requires terminal_id + pane_id and no pid")
		}
	case "process":
		if seat.PID <= 0 || seat.TerminalID != "" || seat.PaneID != "" {
			return fmt.Errorf("process seat requires pid and no terminal/pane")
		}
	default:
		return fmt.Errorf("invalid seat kind %q", seat.Kind)
	}
	return nil
}

func sameSeatBindingProjection(a, b *v2.Seat) bool {
	return reflect.DeepEqual(bindingSeatValue(a), bindingSeatValue(b))
}

func bindingSeatValue(seat *v2.Seat) *v2.BindingSeat {
	if seat == nil {
		return nil
	}
	return &v2.BindingSeat{
		Kind:       seat.Kind,
		Node:       seat.Node,
		TerminalID: seat.TerminalID,
		PaneID:     seat.PaneID,
		PID:        seat.PID,
		Namespace:  seat.Namespace,
	}
}

func busBindingProjection(seat *v2.Seat) (string, bool) {
	if seat == nil || seat.HcomVerified == nil {
		return "", false
	}
	return seat.HcomName, *seat.HcomVerified
}

func hasMatchingSeatBinding(facts []v2.BindingFact, seat *v2.Seat) bool {
	want := bindingSeatValue(seat)
	for _, fact := range facts {
		if fact.Field == v2.BindingFieldSeat && reflect.DeepEqual(fact.Seat, want) {
			return true
		}
	}
	return false
}

func hasMatchingBusBinding(facts []v2.BindingFact, name string) bool {
	for _, fact := range facts {
		if fact.Field == v2.BindingFieldHcomName && fact.Value == name {
			return true
		}
	}
	return false
}

func cloneBindings(in []v2.BindingFact) []v2.BindingFact {
	if len(in) == 0 {
		return nil
	}
	out := make([]v2.BindingFact, len(in))
	copy(out, in)
	for i := range out {
		if in[i].Seat != nil {
			seat := *in[i].Seat
			out[i].Seat = &seat
		}
	}
	return out
}

func cloneAttestations(in []v2.Attestation) []v2.Attestation {
	if len(in) == 0 {
		return nil
	}
	out := make([]v2.Attestation, len(in))
	copy(out, in)
	return out
}

func cloneTombstones(in []v2.BindingTombstone) []v2.BindingTombstone {
	if len(in) == 0 {
		return nil
	}
	out := make([]v2.BindingTombstone, len(in))
	copy(out, in)
	return out
}

func validateDurableMission(row v2.SessionRecord) error {
	if row.Mission == nil {
		return nil
	}
	if row.Mission.Slug == "" {
		return fmt.Errorf("session %s has mission membership without a slug", row.GUID)
	}
	if err := missioncontext.ValidateSlug(row.Mission.Slug); err != nil {
		return fmt.Errorf("session %s has invalid mission membership: %w", row.GUID, err)
	}
	if row.Mission.Source != "explicit" {
		return fmt.Errorf("session %s has invalid durable mission source %q", row.GUID, row.Mission.Source)
	}
	return nil
}

func clearsMission(row v2.SessionRecord) bool {
	if row.Event == "mission_left" || row.Event == "adoption_source_released" {
		return true
	}
	return row.Event == "unseated" && row.Lineage.DisplacedBy != ""
}

func missionChangeEvent(row v2.SessionRecord) bool {
	if row.Event == "mission_joined" || row.Event == "mission_left" || row.Event == "adoption_source_released" {
		return true
	}
	return row.Event == "unseated" && row.Lineage.DisplacedBy != ""
}

func isLegacyV1SessionAppend(row v2.SessionRecord) bool {
	return row.LegacyV1 || row.Event == "legacy_v1_mapped"
}

func carryRegisteredFields(row, current v2.SessionRecord) v2.SessionRecord {
	carriedHcomName := current.Seat != nil && current.Seat.HcomName != "" && (row.Seat == nil || row.Seat.HcomName == "")
	if row.Seat == nil {
		row.State = current.State
	}
	row.Label = firstNonEmpty(row.Label, current.Label)
	row.Role = firstNonEmpty(row.Role, current.Role)
	row.Tool = firstNonEmpty(row.Tool, current.Tool)
	row = carryPiFacts(row, current)
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
	row.Capabilities = carryCapabilities(row.Capabilities, current.Capabilities)
	row.Seat = mergeSeatFields(row.Seat, current.Seat)
	if carriedHcomName && row.Seat != nil {
		verified := false
		row.Seat.HcomVerified = &verified
	}
	return row
}

func mergeSeatFields(patch, current *v2.Seat) *v2.Seat {
	if patch == nil {
		return cloneSeat(current)
	}
	if current == nil {
		seat := cloneSeat(patch)
		defaultSeatVerification(seat)
		return seat
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
		if patch.HcomVerified == nil {
			verified := false
			seat.HcomVerified = &verified
		}
	}
	if patch.HcomVerified != nil {
		seat.HcomVerified = patch.HcomVerified
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

func defaultSeatVerification(seat *v2.Seat) {
	if seat == nil || seat.HcomName == "" || seat.HcomVerified != nil {
		return
	}
	verified := false
	seat.HcomVerified = &verified
}

func cloneSeat(seat *v2.Seat) *v2.Seat {
	if seat == nil {
		return nil
	}
	cp := *seat
	return &cp
}

func carryCapabilities(patch, current *v2.Capabilities) *v2.Capabilities {
	if patch != nil {
		cp := *patch
		return &cp
	}
	if current == nil {
		return nil
	}
	cp := *current
	return &cp
}

func carryIdentityFields(row, current v2.SessionRecord) v2.SessionRecord {
	row.Label = current.Label
	return carryUnlabelledIdentityFields(row, current)
}

func carryUnlabelledIdentityFields(row, current v2.SessionRecord) v2.SessionRecord {
	row.Role = firstNonEmpty(row.Role, current.Role)
	row.Tool = firstNonEmpty(row.Tool, current.Tool)
	row = carryPiFacts(row, current)
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
	row.Capabilities = carryCapabilities(row.Capabilities, current.Capabilities)
	return row
}

func carrySeatFields(row, current v2.SessionRecord) v2.SessionRecord {
	row.Role = firstNonEmpty(row.Role, current.Role)
	row.Tool = firstNonEmpty(row.Tool, current.Tool)
	row = carryPiFacts(row, current)
	row.State = current.State
	row.Seat = cloneSeat(current.Seat)
	if row.Seat == nil && current.LegacyV1 {
		legacy, _ := DecodeLegacyV1Raw(current)
		if legacy.PaneID != "" || legacy.TerminalID != "" || legacy.HcomName != "" || legacy.HcomDir != "" {
			row.State = v2.StateSeated
			row.Seat = &v2.Seat{
				Kind:         "herdr",
				TerminalID:   legacy.TerminalID,
				PaneID:       legacy.PaneID,
				HcomName:     legacy.HcomName,
				HcomVerified: legacy.HcomVerified,
				Namespace:    legacy.HcomDir,
				ConfirmedAt:  row.RecordedAt,
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
	row.Capabilities = carryCapabilities(row.Capabilities, current.Capabilities)
	return row
}

func sameProjectedSession(a, b v2.SessionRecord) bool {
	return a.Kind == b.Kind &&
		a.GUID == b.GUID &&
		a.State == b.State &&
		a.Label == b.Label &&
		a.Role == b.Role &&
		a.Tool == b.Tool &&
		a.Provider == b.Provider &&
		a.Model == b.Model &&
		sameVendorVersion(a.VendorVersion, b.VendorVersion) &&
		sameSeatFields(a.Seat, b.Seat) &&
		reflect.DeepEqual(a.Bindings, b.Bindings) &&
		reflect.DeepEqual(a.Attestations, b.Attestations) &&
		reflect.DeepEqual(a.BindingTombstones, b.BindingTombstones) &&
		sameSIDs(a.SIDs, b.SIDs) &&
		a.Continuity == b.Continuity &&
		a.Lineage == b.Lineage &&
		a.Provenance == b.Provenance &&
		sameCapabilities(a.Capabilities, b.Capabilities) &&
		sameMission(a.Mission, b.Mission) &&
		a.CloseResult == b.CloseResult &&
		a.CloseReason == b.CloseReason
}

func sameMission(a, b *v2.Mission) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func cloneMission(mission *v2.Mission) *v2.Mission {
	if mission == nil {
		return nil
	}
	cp := *mission
	return &cp
}

func sameCapabilities(a, b *v2.Capabilities) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
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
		sameOptionalBool(a.HcomVerified, b.HcomVerified) &&
		a.HooksBound == b.HooksBound &&
		a.TranscriptPath == b.TranscriptPath &&
		a.Namespace == b.Namespace &&
		a.HcomEpoch == b.HcomEpoch &&
		a.HerdrEpoch == b.HerdrEpoch &&
		a.ConfirmedAt == b.ConfirmedAt
}

func carryPiFacts(row, current v2.SessionRecord) v2.SessionRecord {
	row.Provider = firstNonEmpty(row.Provider, current.Provider)
	row.Model = firstNonEmpty(row.Model, current.Model)
	if row.VendorVersion == nil {
		row.VendorVersion = cloneVendorVersion(current.VendorVersion)
	}
	return row
}

func sameVendorVersion(a, b *v2.VendorVersionHistory) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Current != b.Current {
		return false
	}
	if a.Previous == nil || b.Previous == nil {
		return a.Previous == nil && b.Previous == nil
	}
	return *a.Previous == *b.Previous
}

func sameOptionalBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
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
