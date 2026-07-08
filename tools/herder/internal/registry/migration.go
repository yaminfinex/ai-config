package registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

const migrationEventV1 = "migrated_v1"

func migrationNeeded(path string, proj *v2.Projection) bool {
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
	return len(proj.Sessions()) < expected
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
	if len(source) > 0 {
		if err := ensureMigrationArchive(archive, source); err != nil {
			return nil, liveProj, err
		}
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
	rows, err := migrationReseedRows(source, sourceProj, nodeID, mintedNodeRow)
	if err != nil {
		return nil, liveProj, err
	}
	var out bytes.Buffer
	for _, row := range rows {
		out.Write(bytes.TrimRight(row, "\n"))
		out.WriteByte('\n')
	}
	if err := f.Truncate(0); err != nil {
		return nil, liveProj, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, liveProj, err
	}
	if _, err := f.Write(out.Bytes()); err != nil {
		return nil, liveProj, err
	}
	if err := f.Sync(); err != nil {
		return nil, liveProj, err
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return nil, liveProj, err
	}
	proj, err := v2.Load(bytes.NewReader(out.Bytes()), v2.LoadOptions{})
	if err != nil {
		return nil, liveProj, err
	}
	return rows, proj, nil
}

func migrationReseedRows(source []byte, proj *v2.Projection, nodeID string, mintedNodeRow []byte) ([][]byte, error) {
	migrationTime := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	var rows [][]byte
	if len(mintedNodeRow) > 0 {
		rows = append(rows, bytes.Clone(mintedNodeRow))
	}
	for _, node := range proj.Nodes() {
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

func lockedFileBytes(f *os.File) ([]byte, error) {
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	return os.ReadFile(f.Name())
}

func ensureMigrationArchive(path string, source []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, source, 0o444); err != nil {
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

func migrationArchivePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty registry path")
	}
	return filepath.Join(filepath.Dir(path), filepath.Base(path)+".archive", "0001-v1-migration.jsonl"), nil
}

func migrationArchivePathUnchecked(path string) string {
	archive, _ := migrationArchivePath(path)
	return archive
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
