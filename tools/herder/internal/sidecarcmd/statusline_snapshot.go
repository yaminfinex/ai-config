package sidecarcmd

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const statuslineSnapshotTickTolerance = 2 * time.Second

type statuslineSnapshotWriter struct {
	dir               string
	cache             map[string]string
	written           map[string]struct{}
	collided          map[string]struct{}
	transitionCleaned bool
	stableKey         string
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
	w.removeUnseen(seen)
}

func (w *statuslineSnapshotWriter) writeCorrelated(row hcomRow, rows []hcomRow, now time.Time) {
	processID, processOK := safeStatuslineName(row.LaunchContext.ProcessID)
	if w == nil || w.dir == "" || !processOK {
		return
	}
	if err := w.writeIfChanged(processID, renderStatuslineSnapshot(row, now)); err != nil {
		return
	}
	w.stableKey = processID
	if !w.transitionCleaned {
		w.cleanupLegacySnapshot(row, rows)
		w.transitionCleaned = true
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
	liveName, _ := statuslineSnapshotName(row)
	return fmt.Sprintf("HCOM_LIVE_NAME=%s\nHCOM_UNREAD=%d\nHCOM_LAST_TS=%d\nHCOM_LAST_AGE_S=%d\n", liveName, unread, lastTS, age)
}

func (w *statuslineSnapshotWriter) writeIfChanged(name, content string) error {
	path := w.path(name)
	if existing, err := os.ReadFile(path); err == nil {
		content = mergeStatuslineSnapshot(existing, []byte(content))
	}
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

func (w *statuslineSnapshotWriter) cleanupLegacySnapshot(row hcomRow, rows []hcomRow) {
	legacyName, ok := safeStatuslineName(row.Name)
	if !ok || legacyName == row.LaunchContext.ProcessID || nameOwnedByAnotherRow(legacyName, row, rows) {
		return
	}
	path := w.path(legacyName)
	if _, tracked := w.written[legacyName]; !tracked {
		existing, err := os.ReadFile(path)
		if err != nil || parseStatuslineSnapshot(existing)["HCOM_LIVE_NAME"] != legacyName {
			return
		}
	}
	w.remove(legacyName)
}

func nameOwnedByAnotherRow(name string, own hcomRow, rows []hcomRow) bool {
	for _, row := range rows {
		if row.LaunchContext.ProcessID == own.LaunchContext.ProcessID {
			continue
		}
		if liveName, ok := statuslineSnapshotName(row); ok && liveName == name {
			return true
		}
	}
	return false
}

func (w *statuslineSnapshotWriter) removeUnseen(seen map[string]struct{}) {
	for name := range w.written {
		if _, ok := seen[name]; ok {
			continue
		}
		_ = os.Remove(w.path(name))
		delete(w.written, name)
		delete(w.cache, name)
	}
}

func (w *statuslineSnapshotWriter) removeOwned() {
	if w == nil || w.stableKey == "" {
		return
	}
	w.remove(w.stableKey)
	w.stableKey = ""
}

func (w *statuslineSnapshotWriter) path(name string) string {
	return filepath.Join(w.dir, name+".env")
}

func equivalentStatuslineSnapshot(existing, next []byte, tolerance time.Duration) bool {
	oldVals := parseStatuslineSnapshot(existing)
	newVals := parseStatuslineSnapshot(next)
	if oldVals["HCOM_LIVE_NAME"] != newVals["HCOM_LIVE_NAME"] {
		return false
	}
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

func mergeStatuslineSnapshot(existing, next []byte) string {
	nextVals := parseStatuslineSnapshot(next)
	existingVals := parseStatuslineSnapshot(existing)
	var b strings.Builder
	if v := nextVals["HCOM_LIVE_NAME"]; v != "" {
		fmt.Fprintf(&b, "HCOM_LIVE_NAME=%s\n", v)
	}
	for _, key := range []string{"HCOM_UNREAD", "HCOM_LAST_TS", "HCOM_LAST_AGE_S"} {
		if v := nextVals[key]; parseSnapshotUint(v) {
			fmt.Fprintf(&b, "%s=%s\n", key, v)
		}
	}
	if v := existingVals["CTX_PCT"]; parseSnapshotPercent(v) {
		fmt.Fprintf(&b, "CTX_PCT=%s\n", v)
	}
	for _, key := range []string{"CTX_TOKENS", "CTX_SIZE", "CTX_TS"} {
		if v := existingVals[key]; parseSnapshotUint(v) {
			fmt.Fprintf(&b, "%s=%s\n", key, v)
		}
	}
	return b.String()
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

func parseSnapshotUint(s string) bool {
	_, ok := parseSnapshotInt(s)
	return ok
}

func parseSnapshotPercent(s string) bool {
	if s == "" {
		return false
	}
	n, err := strconv.ParseFloat(s, 64)
	return err == nil && !math.IsInf(n, 0) && !math.IsNaN(n) && n >= 0 && n <= 100
}
