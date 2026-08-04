package sidecarcmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// busStamp is the observed identity of the hcom database files. Two equal
// stamps mean `hcom list` would return byte-identical data, so the fork can be
// skipped. WAL mode makes hcom.db-wal the write-signal file; the main db only
// moves on checkpoint, so both are folded in.
type busStamp struct {
	ok      bool
	dbMod   time.Time
	dbSize  int64
	walMod  time.Time
	walSize int64
}

func readBusStamp(hcomDir string) busStamp {
	if hcomDir == "" {
		return busStamp{}
	}
	db, err := os.Stat(filepath.Join(hcomDir, "hcom.db"))
	if err != nil {
		return busStamp{}
	}
	stamp := busStamp{ok: true, dbMod: db.ModTime(), dbSize: db.Size()}
	if wal, err := os.Stat(filepath.Join(hcomDir, "hcom.db-wal")); err == nil {
		stamp.walMod = wal.ModTime()
		stamp.walSize = wal.Size()
	}
	return stamp
}

func (a busStamp) equal(b busStamp) bool {
	return a.ok && b.ok && a.dbSize == b.dbSize && a.walSize == b.walSize &&
		a.dbMod.Equal(b.dbMod) && a.walMod.Equal(b.walMod)
}

// busChanged reports whether the hcom database moved since the last fetch.
// An unreadable stamp fails open (fetch): the refresh cadence still bounds the
// fork rate, so a nonstandard HCOM_DIR layout degrades to pre-gating behavior.
func (s *sidecar) busChanged() bool {
	current := readBusStamp(os.Getenv("HCOM_DIR"))
	return !current.equal(s.busStamp)
}

// busRows fetches the roster. The stamp is read BEFORE the fork: a write
// landing between stamp and fetch makes the next stamp differ, so it can only
// cause one extra fetch, never a missed one.
func (s *sidecar) busRows() []hcomRow {
	s.busStamp = readBusStamp(os.Getenv("HCOM_DIR"))
	s.rowsFetchedAt = time.Now()
	if s.hcomBin == "" {
		s.hcomBin = resolveRealHcom()
	}
	cmd := exec.Command(s.hcomBin, "list", "--json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil
	}
	var rows []hcomRow
	if json.Unmarshal(stdout.Bytes(), &rows) != nil {
		return nil
	}
	return rows
}

// resolveRealHcom walks PATH for hcom, skipping herder path shims by their
// marker line — the shim would route through `bin/herder hook`, whose bash
// wrapper re-hashes the whole source tree on every invocation. For `list`,
// `herder hook` is a byte-faithful passthrough, so calling the real binary
// directly is equivalent and orders of magnitude cheaper.
func resolveRealHcom() string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, "hcom")
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		if isHerderPathShim(candidate) {
			continue
		}
		return candidate
	}
	return "hcom"
}

func isHerderPathShim(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := f.Read(head)
	return bytes.Contains(head[:n], []byte("herder-path-shim"))
}
