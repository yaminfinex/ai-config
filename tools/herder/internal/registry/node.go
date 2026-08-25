package registry

import "path/filepath"

func nodeMarkerPath(registryPath string) string {
	return filepath.Join(filepath.Dir(registryPath), "node_id")
}
