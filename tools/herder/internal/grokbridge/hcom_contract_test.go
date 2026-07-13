package grokbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func installedHcom(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("HERDER_TEST_HCOM_BIN"); p != "" {
		return p
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, "hcom")
		if strings.Contains(p, "tools/herder/shims") {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.Mode()&0o111 != 0 {
			return p
		}
	}
	t.Fatal("real hcom binary unavailable; install hcom 0.7.23 or set HERDER_TEST_HCOM_BIN")
	return ""
}
func hrunProcess(t *testing.T, bin, dir, processID string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	env := scrubEnv(os.Environ(), "HCOM_PROCESS_ID", "CODEX_THREAD_ID")
	env = replaceEnv(env, "HCOM_DIR", dir)
	env = replaceEnv(env, "HCOM_PROCESS_ID", processID)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hcom %v: %v: %s", args, err, out)
	}
	return string(out)
}
func startName(t *testing.T, out string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^\[hcom:([A-Za-z0-9-]+)\]`).FindStringSubmatch(out)
	if len(m) != 2 {
		t.Fatalf("start output has no name: %s", out)
	}
	return m[1]
}
func hrun(t *testing.T, bin, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = replaceEnv(os.Environ(), "HCOM_DIR", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hcom %v: %v: %s", args, err, out)
	}
	return string(out)
}
func hstart(t *testing.T, bin, dir string) string {
	t.Helper()
	return startName(t, hrun(t, bin, dir, "start"))
}

func processBindings(t *testing.T, db string) map[string]string {
	t.Helper()
	cmd := exec.Command("python3", "-c", `import json,sqlite3,sys; c=sqlite3.connect(sys.argv[1]); print(json.dumps(dict(c.execute("select process_id,instance_name from process_bindings"))))`, db)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read bindings: %v: %s", err, out)
	}
	var got map[string]string
	if err = json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func shortState(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "gbs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestRealHcomBindIdentityUsesSeatOwnedProcessAndPreservesForeignBinding(t *testing.T) {
	bin := installedHcom(t)
	bus := t.TempDir()
	foreignName := startName(t, hrunProcess(t, bin, bus, "foreign-process", "start"))
	state := shortState(t)
	b, err := OpenBinder(BinderConfig{Seat: "seat-guid", StateDir: state, HcomBin: bin, HcomDir: bus})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	seatName, err := b.bindIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := processBindings(t, filepath.Join(bus, "hcom.db"))
	if got["foreign-process"] != foreignName || got["seat-guid"] != seatName {
		t.Fatalf("bindings after start=%v, want foreign preserved and seat-owned binding", got)
	}
	reclaimed, err := b.bindIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed != seatName {
		t.Fatalf("reclaimed %q, want %q", reclaimed, seatName)
	}
	got = processBindings(t, filepath.Join(bus, "hcom.db"))
	if got["foreign-process"] != foreignName || got["seat-guid"] != seatName {
		t.Fatalf("bindings after reclaim=%v, want both preserved", got)
	}
}

func TestReadInvocationChildEnvironmentScrubsPinnedIdentityInputs(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "env")
	bin := filepath.Join(t.TempDir(), "hcom")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n/usr/bin/env > \"$GROK_ENV_CAPTURE\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GROK_ENV_CAPTURE", capture)
	t.Setenv("HCOM_PROCESS_ID", "ambient-process")
	t.Setenv("CODEX_THREAD_ID", "ambient-thread")
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: shortState(t), HcomBin: bin, BusName: "seat"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err = b.events(context.Background(), false, 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "HCOM_PROCESS_ID=") || strings.HasPrefix(line, "CODEX_THREAD_ID=") {
			t.Fatalf("identity input leaked into read child env: %q", line)
		}
	}
}
func hsend(t *testing.T, bin, dir, from string, to []string, extra []string, text string) {
	t.Helper()
	args := []string{"send"}
	for _, x := range to {
		args = append(args, "@"+x)
	}
	args = append(args, "--name", from)
	args = append(args, extra...)
	args = append(args, "--", text)
	hrun(t, bin, dir, args...)
}
func unread(t *testing.T, bin, dir, name string) int {
	t.Helper()
	var rows []struct {
		Name   string `json:"name"`
		Unread int    `json:"unread_count"`
	}
	if err := json.Unmarshal([]byte(hrun(t, bin, dir, "list", "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Name == name {
			return r.Unread
		}
	}
	t.Fatalf("identity %s absent", name)
	return 0
}
func testBinder(t *testing.T, bin, bus, seat string) *Binder {
	t.Helper()
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: shortState(t), HcomBin: bin, HcomDir: bus, BusName: seat, Wait: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func TestT24RealHcomStaleBacklogComesFromDrain(t *testing.T) {
	if testing.Short() {
		t.Skip("real hcom contract")
	}
	bin := installedHcom(t)
	bus := t.TempDir()
	seat := hstart(t, bin, bus)
	peer := hstart(t, bin, bus)
	hsend(t, bin, bus, peer, []string{seat}, nil, "stale")
	time.Sleep(11 * time.Second)
	b := testBinder(t, bin, bus, seat)
	rows, err := b.events(context.Background(), true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatal("wait unexpectedly supplied stale backlog")
	}
	if err = b.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.journal.Cursor() == 0 {
		t.Fatal("non-wait drain did not journal stale backlog")
	}
}

func TestT25RealHcomReadsAreIdentityFreeAndNonDestructive(t *testing.T) {
	if testing.Short() {
		t.Skip("real hcom contract")
	}
	bin := installedHcom(t)
	bus := t.TempDir()
	seat := hstart(t, bin, bus)
	peer := hstart(t, bin, bus)
	hsend(t, bin, bus, peer, []string{seat}, nil, "pending")
	before := unread(t, bin, bus, seat)
	t.Setenv("HCOM_PROCESS_ID", "must-be-scrubbed")
	t.Setenv("CODEX_THREAD_ID", "must-be-scrubbed")
	b := testBinder(t, bin, bus, seat)
	rows, err := b.events(context.Background(), false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID == 0 {
		t.Fatalf("full rows=%+v", rows)
	}
	if !strings.Contains(string(rows[0].Raw), `"instance"`) || !strings.Contains(string(rows[0].Raw), `"ts"`) {
		t.Fatalf("raw --full envelope was not preserved: %s", rows[0].Raw)
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(rows[0].Raw, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"delivered_to", "scope", "mentions", "sender_kind"} {
		if _, ok := envelope.Data[key]; !ok {
			t.Fatalf("--full envelope missing %q: %s", key, rows[0].Raw)
		}
	}
	after := unread(t, bin, bus, seat)
	if before != after {
		t.Fatalf("anonymous read changed unread %d -> %d", before, after)
	}
}

func TestT26RealHcomDeliveredToRoutingExcludesSelf(t *testing.T) {
	if testing.Short() {
		t.Skip("real hcom contract")
	}
	bin := installedHcom(t)
	bus := t.TempDir()
	t.Setenv("HCOM_TAG", "group")
	seat := hstart(t, bin, bus)
	peer := hstart(t, bin, bus)
	tag := exec.Command("python3", "-c", `import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); c.execute("update instances set tag='group' where name=?",(sys.argv[2],)); c.commit()`, filepath.Join(bus, "hcom.db"), seat)
	if out, err := tag.CombinedOutput(); err != nil {
		t.Fatalf("seed tag: %v: %s", err, out)
	}
	hsend(t, bin, bus, seat, nil, nil, "self-broadcast")
	hsend(t, bin, bus, peer, []string{seat}, []string{"--thread", "shared"}, "peer-thread")
	hsend(t, bin, bus, seat, nil, []string{"--thread", "shared"}, "self-thread")
	hsend(t, bin, bus, peer, nil, nil, "peer-broadcast")
	hsend(t, bin, bus, peer, []string{"group-"}, nil, "peer-tag")
	hsend(t, bin, bus, peer, []string{seat}, nil, "peer-direct")
	b := testBinder(t, bin, bus, seat)
	if err := b.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ := b.journal.Pending(b.generation, false)
	texts := map[string]bool{}
	for _, r := range p {
		texts[r.Message.Text] = true
	}
	if texts["self-broadcast"] || texts["self-thread"] {
		t.Fatalf("self delivery entered spool: %v", texts)
	}
	for _, want := range []string{"peer-thread", "peer-broadcast", "peer-tag", "peer-direct"} {
		if !texts[want] {
			t.Fatalf("missing routed payload %q: %v", want, texts)
		}
	}
}

func TestT27RealHcomPagedHostileOrderingSurvivesPrefixCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("real hcom contract")
	}
	bin := installedHcom(t)
	bus := t.TempDir()
	seat := hstart(t, bin, bus)
	peer := hstart(t, bin, bus)
	for i := 0; i < 27; i++ {
		hsend(t, bin, bus, peer, []string{seat}, nil, fmt.Sprintf("payload-%02d", i))
	}
	py := exec.Command("python3", "-c", `import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); c.execute("update events set timestamp='2000-01-01T00:00:00+00:00' where type='message'"); c.commit()`, filepath.Join(bus, "hcom.db"))
	if out, err := py.CombinedOutput(); err != nil {
		t.Fatalf("force timestamps: %v: %s", err, out)
	}
	state := shortState(t)
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: bin, HcomDir: bus, BusName: seat})
	if err != nil {
		t.Fatal(err)
	}
	crash := errors.New("injected crash after durable prefix")
	b.afterAppend = func(count int, _ Receipt) error {
		if count == 7 {
			return crash
		}
		return nil
	}
	err = b.Drain(context.Background())
	if !errors.Is(err, crash) {
		t.Fatalf("Drain error=%v, want injected crash", err)
	}
	prefix := b.journal.Cursor()
	b.Close()
	b, err = OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: bin, HcomDir: bus, BusName: seat})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = b.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	ids, texts := rawQueuedRecords(t, filepath.Join(state, "grok", "seat", "journal.jsonl"))
	if len(ids) != 27 {
		t.Fatalf("raw journal queued %d, want 27 (prefix cursor %d): %v", len(ids), prefix, ids)
	}
	seen := map[string]bool{}
	for _, text := range texts {
		if seen[text] {
			t.Fatalf("duplicate payload %q", text)
		}
		seen[text] = true
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("raw queued ids not strictly ascending: %v", ids)
		}
	}
	for i := 0; i < 27; i++ {
		if !seen["payload-"+fmt.Sprintf("%02d", i)] {
			t.Fatal("missing payload " + strconv.Itoa(i))
		}
	}
}

func rawQueuedRecords(t *testing.T, path string) ([]int64, []string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var ids []int64
	var texts []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		var rec Record
		if err = json.Unmarshal(s.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Kind != "queued" {
			continue
		}
		var ev Event
		if err = json.Unmarshal(rec.Event, &ev); err != nil {
			t.Fatal(err)
		}
		var msg Message
		if err = json.Unmarshal(ev.Data, &msg); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.ID)
		texts = append(texts, msg.Text)
	}
	if err = s.Err(); err != nil {
		t.Fatal(err)
	}
	return ids, texts
}
