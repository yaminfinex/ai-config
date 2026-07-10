package registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

const migrationEventV1 = "migrated_v1"
const defaultRotationThresholdBytes int64 = 8 * 1024 * 1024
const rotationThresholdEnv = "HERDER_REGISTRY_ROTATE_BYTES"

var rotationThrashWarnOnce sync.Once

func migrationNeeded(path string, proj *v2.Projection) bool {
	if _, ok := latestRotationArchivePath(path); ok {
		return false
	}
	for _, rec := range proj.Sessions() {
		if rec.LegacyV1 {
			return true
		}
	}
	archive, err := migrationArchivePath(path)
	if err != nil {
		return false
	}
	if _, err := os.Stat(archive); err != nil {
		return false
	}
	archProj, err := v2.LoadFile(archive, v2.LoadOptions{})
	if err != nil {
		return false
	}
	expected := nonRetiredSessionCount(archProj)
	if expected == 0 {
		return false
	}
	if len(proj.Sessions()) == 0 {
		return true
	}
	return migrationPartialLive(proj, expected)
}

func refuseFirstV1MigrationForBornV2(path string, f *os.File, proj *v2.Projection) error {
	if !projectionHasLegacyV1(proj) || len(proj.Nodes()) == 0 {
		return nil
	}
	archive, err := migrationArchivePath(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(archive); err == nil {
		archProj, err := v2.LoadFile(archive, v2.LoadOptions{})
		if err != nil {
			return err
		}
		for _, rec := range archProj.Sessions() {
			if rec.LegacyV1 {
				return &LegacyV1AppendError{GUID: rec.GUID, ArchivePath: archive}
			}
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, _, _, err := ensureLockedNode(path, f, proj); err != nil {
		return err
	}
	for _, rec := range proj.Sessions() {
		if rec.LegacyV1 {
			return &LegacyV1AppendError{GUID: rec.GUID}
		}
	}
	return nil
}

func ensureMigrationNode(path string, proj *v2.Projection) (string, []byte, error) {
	markerPath := NodeMarkerPath(path)
	marker, markerPresent, err := readNodeMarker(markerPath)
	if err != nil {
		return "", nil, err
	}
	nodes := proj.Nodes()
	if markerPresent {
		if hasNode(nodes, marker) {
			return marker, nil, nil
		}
		if len(nodes) == 0 {
			if _, err := os.Stat(migrationArchivePathUnchecked(path)); err != nil {
				return "", nil, nodeGateError(marker, true, nodes)
			}
			row, err := nodeRowBytes(marker)
			return marker, row, err
		}
		return "", nil, nodeGateError(marker, true, nodes)
	}
	if len(nodes) > 0 {
		return "", nil, nodeGateError("", false, nodes)
	}
	nodeID, err := NewGUID()
	if err != nil {
		return "", nil, err
	}
	row, err := nodeRowBytes(nodeID)
	if err != nil {
		return "", nil, err
	}
	if err := writeNodeMarker(markerPath, nodeID); err != nil {
		return "", nil, err
	}
	return nodeID, row, nil
}

func migrateLegacyV1Locked(path string, f *os.File, liveProj *v2.Projection, nodeID string, mintedNodeRow []byte) ([][]byte, *v2.Projection, error) {
	source, err := lockedFileBytes(f)
	if err != nil {
		return nil, liveProj, err
	}
	archive, err := migrationArchivePath(path)
	if err != nil {
		return nil, liveProj, err
	}
	if projectionHasLegacyV1(liveProj) {
		if err := ensureMigrationArchive(archive, source); err != nil {
			return nil, liveProj, err
		}
	} else {
		archived, err := validatedRecoveryArchive(archive, liveProj)
		if err != nil {
			return nil, liveProj, err
		}
		source = archived
	}
	if archived, err := os.ReadFile(archive); err == nil && len(archived) > 0 {
		source = archived
	}
	if len(source) == 0 {
		return nil, liveProj, fmt.Errorf("registry migration refused: %s is empty and no v1 archive exists", path)
	}
	sourceProj, err := v2.Load(bytes.NewReader(source), v2.LoadOptions{})
	if err != nil {
		return nil, liveProj, err
	}
	return rewriteReseedLocked(path, f, source, sourceProj, nodeID, mintedNodeRow)
}

func rotationRecoveryNeeded(path string, liveProj *v2.Projection) bool {
	archive, ok := latestRotationArchivePath(path)
	if !ok {
		return false
	}
	archProj, err := v2.LoadFile(archive, v2.LoadOptions{})
	if err != nil {
		return len(liveProj.Sessions()) == 0
	}
	if len(liveProj.Sessions()) == 0 {
		return true
	}
	expected := nonRetiredSessionCount(archProj)
	return expected > 0 && len(liveProj.Sessions()) < expected
}

func recoverRotationLocked(path string, f *os.File, liveProj *v2.Projection) ([][]byte, *v2.Projection, error) {
	archive, ok := latestRotationArchivePath(path)
	if !ok {
		return nil, liveProj, fmt.Errorf("registry rotation recovery refused: no archive exists for %s", path)
	}
	source, err := validatedRecoveryArchive(archive, liveProj)
	if err != nil {
		return nil, liveProj, err
	}
	sourceProj, err := v2.Load(bytes.NewReader(source), v2.LoadOptions{})
	if err != nil {
		return nil, liveProj, err
	}
	nodeID, mintedRow, err := recoveryNode(path, liveProj, sourceProj)
	if err != nil {
		return nil, liveProj, err
	}
	return rewriteReseedLocked(path, f, source, sourceProj, nodeID, mintedRow)
}

func rotateIfNeededLocked(path string, f *os.File, proj *v2.Projection, nodeID string) ([][]byte, *v2.Projection, error) {
	threshold, err := rotationThresholdBytes()
	if err != nil {
		return nil, proj, err
	}
	if threshold <= 0 {
		return nil, proj, nil
	}
	info, err := f.Stat()
	if err != nil {
		return nil, proj, err
	}
	if info.Size() <= threshold {
		return nil, proj, nil
	}
	source, err := lockedFileBytes(f)
	if err != nil {
		return nil, proj, err
	}
	rows, out, err := reseedBytes(source, proj, nodeID, nil)
	if err != nil {
		return nil, proj, err
	}
	if int64(len(out)) > threshold {
		rotationThrashWarnOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "herder registry rotation skipped for %s: reseed snapshot is %d bytes, still above %s=%d; leaving live file unrotated\n", path, len(out), rotationThresholdEnv, threshold)
		})
		return nil, proj, nil
	}
	if _, ok := matchingRegistryArchivePath(path, source); ok {
		return rewriteReseedBytesLocked(path, f, rows, out, proj)
	}
	archive, err := nextRotationArchivePath(path)
	if err != nil {
		return nil, proj, err
	}
	if err := ensureArchive(archive, source); err != nil {
		return nil, proj, err
	}
	archived, err := os.ReadFile(archive)
	if err != nil {
		return nil, proj, err
	}
	if !bytes.Equal(archived, source) {
		return nil, proj, fmt.Errorf("registry rotation refused: archive %s byte verification failed", archive)
	}
	return rewriteReseedBytesLocked(path, f, rows, out, proj)
}

func rewriteReseedLocked(path string, f *os.File, source []byte, sourceProj *v2.Projection, nodeID string, mintedNodeRow []byte) ([][]byte, *v2.Projection, error) {
	rows, out, err := reseedBytes(source, sourceProj, nodeID, mintedNodeRow)
	if err != nil {
		return nil, sourceProj, err
	}
	return rewriteReseedBytesLocked(path, f, rows, out, sourceProj)
}

func reseedBytes(source []byte, sourceProj *v2.Projection, nodeID string, mintedNodeRow []byte) ([][]byte, []byte, error) {
	rows, err := migrationReseedRows(source, sourceProj, nodeID, mintedNodeRow)
	if err != nil {
		return nil, nil, err
	}
	var out bytes.Buffer
	for _, row := range rows {
		out.Write(bytes.TrimRight(row, "\n"))
		out.WriteByte('\n')
	}
	return rows, out.Bytes(), nil
}

func rewriteReseedBytesLocked(path string, f *os.File, rows [][]byte, out []byte, prev *v2.Projection) ([][]byte, *v2.Projection, error) {
	if err := f.Truncate(0); err != nil {
		return nil, prev, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, prev, err
	}
	if _, err := f.Write(out); err != nil {
		return nil, prev, err
	}
	if err := f.Sync(); err != nil {
		return nil, prev, err
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return nil, prev, err
	}
	proj, err := v2.Load(bytes.NewReader(out), v2.LoadOptions{})
	if err != nil {
		return nil, prev, err
	}
	return rows, proj, nil
}

func migrationReseedRows(source []byte, proj *v2.Projection, nodeID string, mintedNodeRow []byte) ([][]byte, error) {
	migrationTime := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	var rows [][]byte
	sawNodeID := false
	for _, node := range proj.Nodes() {
		if node.NodeID == nodeID {
			sawNodeID = true
		}
		if len(node.Raw) > 0 {
			rows = append(rows, bytes.Clone(node.Raw))
			continue
		}
		b, err := json.Marshal(node)
		if err != nil {
			return nil, err
		}
		rows = append(rows, b)
	}
	if !sawNodeID {
		if len(mintedNodeRow) == 0 {
			var err error
			mintedNodeRow, err = nodeRowBytes(nodeID)
			if err != nil {
				return nil, err
			}
		}
		rows = append(rows, bytes.Clone(mintedNodeRow))
	}
	namespaceRows, err := migrationNamespaceRows(source, proj, nodeID, migrationTime)
	if err != nil {
		return nil, err
	}
	rows = append(rows, namespaceRows...)
	for _, epoch := range proj.Epochs() {
		if len(epoch.Raw) > 0 {
			rows = append(rows, bytes.Clone(epoch.Raw))
			continue
		}
		b, err := json.Marshal(epoch)
		if err != nil {
			return nil, err
		}
		rows = append(rows, b)
	}
	for _, rec := range proj.Sessions() {
		if rec.State == v2.StateRetired || rec.State == v2.StateLost {
			continue
		}
		rec.Raw = nil
		rec.Ordinal = 0
		rec.LegacyV1 = false
		if rec.Kind == "" {
			rec.Kind = v2.KindSession
		}
		if rec.LegacyV1 || rec.Event == "legacy_v1_mapped" {
			rec.Event = migrationEventV1
			rec.RecordedAt = migrationTime
			rec.State = v2.StateUnseated
			rec.Seat = nil
		}
		if rec.Event == "" {
			rec.Event = migrationEventV1
		}
		if rec.RecordedAt == "" {
			rec.RecordedAt = migrationTime
		}
		rec.Node = nodeID
		if rec.Seat != nil {
			rec.Seat.Node = nodeID
		}
		b, err := json.Marshal(rec)
		if err != nil {
			return nil, err
		}
		rows = append(rows, b)
	}
	return rows, nil
}

func migrationNamespaceRows(source []byte, proj *v2.Projection, nodeID, recordedAt string) ([][]byte, error) {
	seen := map[string]bool{}
	var rows [][]byte
	for _, ns := range proj.Namespaces() {
		seen[ns.Path] = true
		if len(ns.Raw) > 0 {
			rows = append(rows, bytes.Clone(ns.Raw))
			continue
		}
		b, err := json.Marshal(ns)
		if err != nil {
			return nil, err
		}
		rows = append(rows, b)
	}
	paths := legacyHcomDirs(source)
	for _, path := range paths {
		if seen[path] {
			continue
		}
		ns := v2.NamespaceRecord{
			Kind:        v2.KindNamespace,
			Event:       "namespace_observed",
			NamespaceID: namespaceIDForPath(nodeID, path),
			Node:        nodeID,
			Path:        path,
			RecordedAt:  recordedAt,
		}
		b, err := json.Marshal(ns)
		if err != nil {
			return nil, err
		}
		rows = append(rows, b)
		seen[path] = true
	}
	return rows, nil
}

func legacyHcomDirs(source []byte) []string {
	seen := map[string]bool{}
	for _, line := range bytes.Split(source, []byte{'\n'}) {
		raw := bytes.TrimSpace(line)
		if len(raw) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		if dir := rawString(obj["hcom_dir"]); dir != "" {
			seen[dir] = true
		}
	}
	out := make([]string, 0, len(seen))
	for dir := range seen {
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

func nonRetiredSessionCount(proj *v2.Projection) int {
	count := 0
	for _, rec := range proj.Sessions() {
		if rec.State != v2.StateRetired && rec.State != v2.StateLost {
			count++
		}
	}
	return count
}

func migrationPartialLive(proj *v2.Projection, expected int) bool {
	sessions := proj.Sessions()
	if len(sessions) >= expected {
		return false
	}
	for _, rec := range sessions {
		if rec.Event != migrationEventV1 {
			return false
		}
	}
	return true
}

func projectionHasLegacyV1(proj *v2.Projection) bool {
	for _, rec := range proj.Sessions() {
		if rec.LegacyV1 {
			return true
		}
	}
	return false
}

func lockedFileBytes(f *os.File) ([]byte, error) {
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	return os.ReadFile(f.Name())
}

func ensureMigrationArchive(path string, source []byte) error {
	return ensureArchive(path, source)
}

func ensureArchive(path string, source []byte) error {
	if _, err := os.Stat(path); err == nil {
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(existing) != len(source) || !bytes.Equal(existing, source) {
			return fmt.Errorf("registry archive byte verification failed: existing archive %s does not match live registry; do not remove the archive. Safe recovery is to back up the registry, identify and excise post-mint v1-shaped rows from the live file, then retry with the verified archive in place", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o444)
	if err != nil {
		return err
	}
	if _, err := f.Write(source); err != nil {
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
	if err := os.Chmod(tmp, 0o444); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func validatedRecoveryArchive(path string, liveProj *v2.Projection) ([]byte, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("registry archive recovery refused: missing archive %s; restore the archive before retrying: %w", path, err)
	}
	if len(source) == 0 {
		return nil, fmt.Errorf("registry archive recovery refused: archive %s is empty; restore the archive before retrying", path)
	}
	proj, err := v2.Load(bytes.NewReader(source), v2.LoadOptions{})
	if err != nil {
		return nil, err
	}
	if len(proj.Quarantined()) > 0 {
		return nil, fmt.Errorf("registry archive recovery refused: archive %s has quarantined rows; restore the archive before retrying", path)
	}
	expected := nonRetiredSessionCount(proj)
	if expected == 0 || len(liveProj.Sessions()) >= expected {
		return nil, fmt.Errorf("registry archive recovery refused: archive %s does not contain enough non-retired sessions to recover the partial live registry", path)
	}
	return source, nil
}

func migrationArchivePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty registry path")
	}
	return filepath.Join(registryArchiveDir(path), "0001-v1-migration.jsonl"), nil
}

func migrationArchivePathUnchecked(path string) string {
	archive, _ := migrationArchivePath(path)
	return archive
}

func registryArchiveDir(path string) string {
	return filepath.Join(filepath.Dir(path), filepath.Base(path)+".archive")
}

func registryArchivePaths(path string) ([]string, error) {
	dir := registryArchiveDir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if archiveSequence(name) == 0 || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	sort.Slice(paths, func(i, j int) bool {
		return archiveSequence(filepath.Base(paths[i])) < archiveSequence(filepath.Base(paths[j]))
	})
	return paths, nil
}

func latestRegistryArchivePath(path string) (string, bool) {
	archives, err := registryArchivePaths(path)
	if err != nil || len(archives) == 0 {
		return "", false
	}
	return archives[len(archives)-1], true
}

func latestRotationArchivePath(path string) (string, bool) {
	archives, err := registryArchivePaths(path)
	if err != nil {
		return "", false
	}
	for i := len(archives) - 1; i >= 0; i-- {
		if archiveSequence(filepath.Base(archives[i])) >= 2 {
			return archives[i], true
		}
	}
	return "", false
}

func matchingRegistryArchivePath(path string, source []byte) (string, bool) {
	archives, err := registryArchivePaths(path)
	if err != nil {
		return "", false
	}
	for i := len(archives) - 1; i >= 0; i-- {
		archived, err := os.ReadFile(archives[i])
		if err == nil && bytes.Equal(archived, source) {
			return archives[i], true
		}
	}
	return "", false
}

func nextRotationArchivePath(path string) (string, error) {
	archives, err := registryArchivePaths(path)
	if err != nil {
		return "", err
	}
	next := 1
	for _, archive := range archives {
		if seq := archiveSequence(filepath.Base(archive)); seq >= next {
			next = seq + 1
		}
	}
	return filepath.Join(registryArchiveDir(path), fmt.Sprintf("%04d-rotation.jsonl", next)), nil
}

func archiveSequence(name string) int {
	if len(name) < 5 || name[4] != '-' {
		return 0
	}
	seq, err := strconv.Atoi(name[:4])
	if err != nil || seq <= 0 {
		return 0
	}
	return seq
}

func rotationThresholdBytes() (int64, error) {
	raw := strings.TrimSpace(os.Getenv(rotationThresholdEnv))
	if raw == "" {
		return defaultRotationThresholdBytes, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: set %s to a positive byte count or unset it to use the default: %w", rotationThresholdEnv, raw, rotationThresholdEnv, err)
	}
	return n, nil
}

func recoveryNode(path string, liveProj, archiveProj *v2.Projection) (string, []byte, error) {
	marker, markerPresent, err := readNodeMarker(NodeMarkerPath(path))
	if err != nil {
		return "", nil, err
	}
	if markerPresent {
		if hasNode(liveProj.Nodes(), marker) || hasNode(archiveProj.Nodes(), marker) {
			return marker, nil, nil
		}
		row, err := nodeRowBytes(marker)
		return marker, row, err
	}
	nodes := archiveProj.Nodes()
	if len(nodes) == 1 {
		nodeID := nodes[0].NodeID
		if err := writeNodeMarker(NodeMarkerPath(path), nodeID); err != nil {
			return "", nil, err
		}
		return nodeID, nil, nil
	}
	return "", nil, nodeGateError("", false, liveProj.Nodes())
}

func nodeRowBytes(nodeID string) ([]byte, error) {
	row := v2.NodeRecord{
		Kind:       v2.KindNode,
		Event:      "node_registered",
		NodeID:     nodeID,
		User:       os.Getenv("USER"),
		Hostname:   hostname(),
		RecordedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	return json.Marshal(row)
}

func namespaceIDForPath(nodeID, path string) string {
	sum := sha256.Sum256([]byte(nodeID + "\x00" + path))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
