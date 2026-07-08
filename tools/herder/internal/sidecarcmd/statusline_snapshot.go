package sidecarcmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const statuslineSnapshotTickTolerance = 2 * time.Second

type statuslineSnapshotWriter struct {
	dir      string
	cache    map[string]string
	written  map[string]struct{}
	collided map[string]struct{}
}

func newStatuslineSnapshotWriter(hcomDir string) *statuslineSnapshotWriter {
	w := &statuslineSnapshotWriter{
		cache:    make(map[string]string),
		written:  make(map[string]struct{}),
		collided: make(map[string]struct{}),
	}
	if hcomDir != "" {
		w.dir = filepath.Join(hcomDir, "statusline")
	}
	return w
}

func (w *statuslineSnapshotWriter) writeRows(rows []hcomRow, now time.Time) {
	if w == nil || w.dir == "" || rows == nil {
		return
	}
	collisions := statuslineSnapshotCollisions(rows)
	seen := make(map[string]struct{})
	for _, row := range rows {
		name, ok := statuslineSnapshotName(row)
		if !ok {
			continue
		}
		seen[name] = struct{}{}
		if _, collision := collisions[name]; collision {
			w.removeCollided(name)
			continue
		}
		delete(w.collided, name)
		content := renderStatuslineSnapshot(row, now)
		_ = w.writeIfChanged(name, content)
	}
	for name := range w.written {
		if _, ok := seen[name]; ok {
			continue
		}
		_ = os.Remove(w.path(name))
		delete(w.written, name)
		delete(w.cache, name)
	}
}

func statuslineSnapshotCollisions(rows []hcomRow) map[string]struct{} {
	counts := make(map[string]int)
	for _, row := range rows {
		name, ok := statuslineSnapshotName(row)
		if !ok {
			continue
		}
		counts[name]++
	}
	collisions := make(map[string]struct{})
	for name, count := range counts {
		if count > 1 {
			collisions[name] = struct{}{}
		}
	}
	return collisions
}

func statuslineSnapshotName(row hcomRow) (string, bool) {
	if row.BaseName != "" {
		return safeStatuslineName(row.BaseName)
	}
	return safeStatuslineName(row.Name)
}

func safeStatuslineName(name string) (string, bool) {
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	if strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") {
		return "", false
	}
	return name, true
}

func defaultStatuslineInstanceName() string {
	return firstNonEmpty(os.Getenv("HCOM_INSTANCE_NAME"), os.Getenv("HCOM_NAME"), "self")
}

func renderStatuslineSnapshot(row hcomRow, now time.Time) string {
	unread := row.UnreadCount
	if unread < 0 {
		unread = 0
	}
	age := row.StatusAgeS
	if age < 0 {
		age = 0
	}
	lastTS := now.Unix() - age
	return fmt.Sprintf("HCOM_UNREAD=%d\nHCOM_LAST_TS=%d\nHCOM_LAST_AGE_S=%d\n", unread, lastTS, age)
}

func (w *statuslineSnapshotWriter) writeIfChanged(name, content string) error {
	path := w.path(name)
	if w.cache[name] == content {
		if _, err := os.Stat(path); err != nil {
			delete(w.cache, name)
		} else {
			w.written[name] = struct{}{}
			return nil
		}
	}
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == content || equivalentStatuslineSnapshot(existing, []byte(content), statuslineSnapshotTickTolerance) {
			w.cache[name] = string(existing)
			w.written[name] = struct{}{}
			return nil
		}
	}
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(w.dir, "."+name+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	w.cache[name] = content
	w.written[name] = struct{}{}
	return nil
}

func (w *statuslineSnapshotWriter) removeCollided(name string) {
	if _, removed := w.collided[name]; removed {
		return
	}
	w.remove(name)
	w.collided[name] = struct{}{}
}

func (w *statuslineSnapshotWriter) remove(name string) {
	if w == nil || w.dir == "" {
		return
	}
	_ = os.Remove(w.path(name))
	delete(w.written, name)
	delete(w.cache, name)
}

func (w *statuslineSnapshotWriter) removeInstance(name string) {
	safeName, ok := safeStatuslineName(name)
	if !ok {
		return
	}
	w.remove(safeName)
}

func (w *statuslineSnapshotWriter) path(name string) string {
	return filepath.Join(w.dir, name+".env")
}

func equivalentStatuslineSnapshot(existing, next []byte, tolerance time.Duration) bool {
	oldVals := parseStatuslineSnapshot(existing)
	newVals := parseStatuslineSnapshot(next)
	if oldVals["HCOM_UNREAD"] == "" || newVals["HCOM_UNREAD"] == "" || oldVals["HCOM_UNREAD"] != newVals["HCOM_UNREAD"] {
		return false
	}
	oldTS, oldOK := parseSnapshotInt(oldVals["HCOM_LAST_TS"])
	newTS, newOK := parseSnapshotInt(newVals["HCOM_LAST_TS"])
	if !oldOK || !newOK {
		return false
	}
	diff := oldTS - newTS
	if diff < 0 {
		diff = -diff
	}
	return diff <= int64(tolerance/time.Second)
}

func parseStatuslineSnapshot(b []byte) map[string]string {
	vals := make(map[string]string)
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		key, value, ok := bytes.Cut(line, []byte{'='})
		if !ok {
			continue
		}
		vals[string(key)] = string(value)
	}
	return vals
}

func parseSnapshotInt(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n >= 0
}
