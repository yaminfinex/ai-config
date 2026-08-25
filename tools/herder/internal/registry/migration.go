package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

const defaultRotationThresholdBytes int64 = 8 * 1024 * 1024
const rotationThresholdEnv = "HERDER_REGISTRY_ROTATE_BYTES"

var rotationThrashWarnOnce sync.Once

// rotateIfNeededLocked compacts the disposable display cache in place. The
// flock held by UpdateLocked makes the truncate-and-reseed indivisible from
// every other registry writer. There is deliberately no archive: the observer
// can reconstruct the cache from live substrate state.
func rotateIfNeededLocked(path string, f *os.File, proj *v2.Projection) (bool, *v2.Projection, error) {
	threshold, err := rotationThresholdBytes()
	if err != nil {
		return false, proj, err
	}
	if threshold <= 0 {
		return false, proj, nil
	}
	info, err := f.Stat()
	if err != nil {
		return false, proj, err
	}
	if info.Size() <= threshold {
		return false, proj, nil
	}

	out, err := compactProjectionBytes(proj)
	if err != nil {
		return false, proj, err
	}
	if int64(len(out)) > threshold {
		rotationThrashWarnOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "herder registry rotation skipped for %s: compact cache is %d bytes, still above %s=%d; leaving live file unrotated\n", path, len(out), rotationThresholdEnv, threshold)
		})
		return false, proj, nil
	}

	if err := f.Truncate(0); err != nil {
		return false, proj, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return false, proj, err
	}
	if _, err := f.Write(out); err != nil {
		return false, proj, err
	}
	if err := f.Sync(); err != nil {
		return false, proj, err
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return false, proj, err
	}
	next, err := v2.Load(bytes.NewReader(out), v2.LoadOptions{})
	if err != nil {
		return false, proj, err
	}
	return true, next, nil
}

func compactProjectionBytes(proj *v2.Projection) ([]byte, error) {
	var out bytes.Buffer
	appendRow := func(value any, raw []byte) error {
		if len(bytes.TrimSpace(raw)) != 0 {
			out.Write(bytes.TrimRight(raw, "\n"))
			out.WriteByte('\n')
			return nil
		}
		row, err := json.Marshal(value)
		if err != nil {
			return err
		}
		out.Write(row)
		out.WriteByte('\n')
		return nil
	}
	for _, row := range proj.Nodes() {
		if err := appendRow(row, row.Raw); err != nil {
			return nil, err
		}
	}
	for _, row := range proj.Namespaces() {
		if err := appendRow(row, row.Raw); err != nil {
			return nil, err
		}
	}
	for _, row := range proj.Epochs() {
		if err := appendRow(row, row.Raw); err != nil {
			return nil, err
		}
	}
	for _, row := range proj.Sessions() {
		if row.State == v2.StateRetired || row.State == v2.StateLost {
			continue
		}
		if err := appendRow(row, row.Raw); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
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
