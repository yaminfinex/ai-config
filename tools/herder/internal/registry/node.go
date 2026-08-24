package registry

import "path/filepath"

func NodeMarkerPath(registryPath string) string {
	return filepath.Join(filepath.Dir(registryPath), "node_id")
}
