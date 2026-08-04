package sidecarcmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBusChangedGatesOnDatabaseStamp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HCOM_DIR", dir)
	db := filepath.Join(dir, "hcom.db")
	if err := os.WriteFile(db, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &sidecar{}
	if !s.busChanged() {
		t.Fatal("first observation must always report change")
	}
	s.busStamp = readBusStamp(dir)
	if s.busChanged() {
		t.Fatal("unchanged database reported change")
	}
	if err := os.WriteFile(db, []byte("grew!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !s.busChanged() {
		t.Fatal("database growth not detected")
	}
	s.busStamp = readBusStamp(dir)
	wal := filepath.Join(dir, "hcom.db-wal")
	if err := os.WriteFile(wal, []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !s.busChanged() {
		t.Fatal("WAL appearance not detected")
	}
}

func TestBusChangedFailsOpenWithoutDatabase(t *testing.T) {
	t.Setenv("HCOM_DIR", filepath.Join(t.TempDir(), "missing"))
	s := &sidecar{busStamp: busStamp{ok: true}}
	if !s.busChanged() {
		t.Fatal("unreadable stamp must fail open to fetching")
	}
	t.Setenv("HCOM_DIR", "")
	if !s.busChanged() {
		t.Fatal("empty HCOM_DIR must fail open to fetching")
	}
}

func TestResolveRealHcomSkipsHerderPathShim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binaries are shell scripts")
	}
	shimDir := t.TempDir()
	realDir := t.TempDir()
	shim := "#!/usr/bin/env bash\n# herder-path-shim: hcom — marker\nexit 1\n"
	if err := os.WriteFile(filepath.Join(shimDir, "hcom"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(realDir, "hcom")
	if err := os.WriteFile(real, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+realDir)
	if got := resolveRealHcom(); got != real {
		t.Fatalf("resolveRealHcom = %q, want %q", got, real)
	}
}

func TestResolveRealHcomFallsBackToBareName(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := resolveRealHcom(); got != "hcom" {
		t.Fatalf("resolveRealHcom fallback = %q, want hcom", got)
	}
}

func TestScheduleNextRefreshBacksOffOnSustainedMiss(t *testing.T) {
	s := &sidecar{}
	now := time.Now()
	var intervals []time.Duration
	for i := 0; i < 12; i++ {
		s.scheduleNextRefresh(now, nil, false)
		intervals = append(intervals, s.nextRefresh.Sub(now))
	}
	for i := 0; i < refreshMissGrace; i++ {
		if intervals[i] != refreshFloor {
			t.Fatalf("miss %d interval = %s, want floor %s within grace", i, intervals[i], refreshFloor)
		}
	}
	if intervals[refreshMissGrace] <= refreshFloor {
		t.Fatalf("backoff did not start after grace: %s", intervals[refreshMissGrace])
	}
	last := intervals[len(intervals)-1]
	if last != refreshCeiling {
		t.Fatalf("sustained miss interval = %s, want ceiling %s", last, refreshCeiling)
	}
	row := &hcomRow{Name: "back"}
	s.scheduleNextRefresh(now, row, true)
	if got := s.nextRefresh.Sub(now); got != refreshHealthy {
		t.Fatalf("healthy interval = %s, want %s", got, refreshHealthy)
	}
	if s.missStreak != 0 {
		t.Fatalf("missStreak = %d after correlate returned, want 0", s.missStreak)
	}
}

func TestRefreshDueSkipsCorrelatedSeatOnQuietBus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HCOM_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "hcom.db"), []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &sidecar{busStamp: readBusStamp(dir)}
	now := time.Now()
	if s.refreshDue(now, true) {
		t.Fatal("correlated seat refreshed despite quiet bus")
	}
	if !s.refreshDue(now, false) {
		t.Fatal("uncorrelated seat must refresh on schedule even when bus is quiet")
	}
	if err := os.WriteFile(filepath.Join(dir, "hcom.db"), []byte("moved"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !s.refreshDue(now, true) {
		t.Fatal("bus movement did not trigger correlated refresh")
	}
	s.nextRefresh = now.Add(time.Hour)
	if s.refreshDue(now, false) {
		t.Fatal("refresh ran before its scheduled time")
	}
}

func TestEffectiveStatusAgeAgesCachedRows(t *testing.T) {
	s := &sidecar{}
	row := &hcomRow{StatusAgeS: 10}
	if got := s.effectiveStatusAge(row); got != 10 {
		t.Fatalf("age without fetch time = %d, want 10", got)
	}
	s.rowsFetchedAt = time.Now().Add(-30 * time.Second)
	if got := s.effectiveStatusAge(row); got < 39 || got > 41 {
		t.Fatalf("aged status = %d, want ~40", got)
	}
}
