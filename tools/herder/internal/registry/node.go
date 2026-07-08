package registry

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

type NodeInitResult struct {
	NodeID  string
	Changed bool
	Message string
}

func NodeMarkerPath(registryPath string) string {
	return filepath.Join(filepath.Dir(registryPath), "node_id")
}

// InitNode is the explicit repair/mint path behind `herder node init`. It uses
// the same registry flock as ordinary writes, but is allowed to repair
// half-present marker/registry state that ordinary writes must refuse.
func InitNode(path string, forceNew bool) (NodeInitResult, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return NodeInitResult{}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return NodeInitResult{}, err
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return NodeInitResult{}, fmt.Errorf("registry lock unavailable for %s: refusing to write unlocked: %w", path, err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return NodeInitResult{}, err
	}
	proj, err := v2.Load(f, v2.LoadOptions{})
	if err != nil {
		return NodeInitResult{}, err
	}
	marker, markerPresent, err := readNodeMarkerLenient(NodeMarkerPath(path))
	if err != nil {
		return NodeInitResult{}, err
	}
	nodes := proj.Nodes()

	malformedMarker := markerPresent && marker != "" && !isNodeIDShape(marker)
	if malformedMarker && (!forceNew || len(nodes) == 1) {
		return NodeInitResult{}, malformedMarkerError(NodeMarkerPath(path), marker, nodes)
	}

	if forceNew {
		nodeID, _, _, err := mintLockedNode(path, f, proj, "")
		if err != nil {
			return NodeInitResult{}, err
		}
		if err := f.Sync(); err != nil {
			return NodeInitResult{}, err
		}
		return NodeInitResult{NodeID: nodeID, Changed: true, Message: "minted fresh node id"}, nil
	}

	validMarker := markerPresent && marker != "" && !malformedMarker

	if validMarker && hasNode(nodes, marker) {
		return NodeInitResult{NodeID: marker, Message: "node id already initialized"}, nil
	}
	if !validMarker && len(nodes) == 0 {
		nodeID, _, _, err := mintLockedNode(path, f, proj, "")
		if err != nil {
			return NodeInitResult{}, err
		}
		if err := f.Sync(); err != nil {
			return NodeInitResult{}, err
		}
		return NodeInitResult{NodeID: nodeID, Changed: true, Message: "minted node id"}, nil
	}
	if validMarker && len(nodes) == 0 {
		if _, _, _, err := mintLockedNode(path, f, proj, marker); err != nil {
			return NodeInitResult{}, err
		}
		if err := f.Sync(); err != nil {
			return NodeInitResult{}, err
		}
		return NodeInitResult{NodeID: marker, Changed: true, Message: "repaired missing node_registered row"}, nil
	}
	if !validMarker && len(nodes) == 1 {
		nodeID := nodes[0].NodeID
		if err := writeNodeMarker(NodeMarkerPath(path), nodeID); err != nil {
			return NodeInitResult{}, err
		}
		return NodeInitResult{NodeID: nodeID, Changed: true, Message: "repaired missing node_id marker"}, nil
	}
	return NodeInitResult{}, fmt.Errorf("registry node init refused: %s; rerun with `herder node init --new` to mint a fresh node id for this state dir", describeNodeState(marker, markerPresent, nodes))
}

func malformedMarkerError(path, marker string, nodes []v2.NodeRecord) error {
	if len(nodes) == 1 {
		return fmt.Errorf("registry node init refused: malformed node marker %s contains %q; restore it from registry node_registered row %s, or rerun with `herder node init --new` for clone repair", path, marker, nodes[0].NodeID)
	}
	return fmt.Errorf("registry node init refused: malformed node marker %s contains %q; rerun with `herder node init --new` when the marker and registry node rows are both bad", path, marker)
}

func describeNodeState(marker string, markerPresent bool, nodes []v2.NodeRecord) string {
	switch {
	case markerPresent && len(nodes) > 0:
		return fmt.Sprintf("marker contains %s but registry node rows are %s", marker, nodeIDs(nodes))
	case !markerPresent && len(nodes) > 1:
		return fmt.Sprintf("registry has multiple node_registered rows (%s) and marker is absent", nodeIDs(nodes))
	case !markerPresent && len(nodes) > 0:
		return fmt.Sprintf("registry has node_registered row %s but marker is absent", nodes[0].NodeID)
	default:
		return "marker and registry node state are inconsistent"
	}
}
