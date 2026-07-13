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
	env := hcomContractEnv(dir)
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
	cmd.Env = hcomContractEnv(dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hcom %v: %v: %s", args, err, out)
	}
	return string(out)
}

func hcomContractEnv(dir string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if isHcomContractIdentityEnv(key) {
			continue
		}
		env = append(env, item)
	}
	return append(env, "HCOM_DIR="+dir)
}

func isHcomContractIdentityEnv(key string) bool {
	return key == "HCOM" || key == "CODEX_THREAD_ID" ||
		strings.HasPrefix(key, "HCOM_") || strings.HasPrefix(key, "CLAUDE")
}

func unsetHcomContractIdentityEnv(t *testing.T) {
	t.Helper()
	type entry struct{ key, value string }
	saved := make([]entry, 0)
	for _, item := range os.Environ() {
		key, value, _ := strings.Cut(item, "=")
		if isHcomContractIdentityEnv(key) {
			saved = append(saved, entry{key: key, value: value})
		}
	}
	t.Cleanup(func() {
		for _, item := range os.Environ() {
			key, _, _ := strings.Cut(item, "=")
			if isHcomContractIdentityEnv(key) {
				if err := os.Unsetenv(key); err != nil {
					t.Errorf("clear contract identity env %s: %v", key, err)
				}
			}
		}
		for _, item := range saved {
			if err := os.Setenv(item.key, item.value); err != nil {
				t.Errorf("restore contract identity env %s: %v", item.key, err)
			}
		}
	})
	for _, item := range saved {
		if err := os.Unsetenv(item.key); err != nil {
			t.Fatalf("unset contract identity env %s: %v", item.key, err)
		}
	}
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

func identityCursorAndRows(t *testing.T, db, name string) (int64, int) {
	t.Helper()
	cmd := exec.Command("python3", "-c", `import json,sqlite3,sys; c=sqlite3.connect(sys.argv[1]); row=c.execute("select coalesce(max(last_event_id),0),count(*) from instances where name=?",(sys.argv[2],)).fetchone(); print(json.dumps(row))`, db, name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read identity state: %v: %s", err, out)
	}
	var got []int64
	if err = json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	return got[0], int(got[1])
}

func ageIdentityPlaceholder(t *testing.T, db, name string) {
	t.Helper()
	cmd := exec.Command("python3", "-c", `import sqlite3,sys,time; c=sqlite3.connect(sys.argv[1]); c.execute("update instances set created_at=? where name=?",(time.time()-31,sys.argv[2])); c.commit()`, db, name)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("age identity placeholder: %v: %s", err, out)
	}
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
	// Helpers stay neutral, while the binder's parent environment deliberately
	// carries the foreign tool signals that would select hcom's hook-install path
	// if the identity invocation inherited them.
	unsetHcomContractIdentityEnv(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("HCOM_TOOL", "foreign-tool")
	t.Setenv("HCOM_TAG", "foreign-tag")
	bin := installedHcom(t)
	bus := t.TempDir()
	foreignName := startName(t, hrunProcess(t, bin, bus, "foreign-process", "start"))
	seatName := startName(t, hrunProcess(t, bin, bus, "seat-process", "start"))
	peerName := startName(t, hrunProcess(t, bin, bus, "peer-process", "start"))
	hsend(t, bin, bus, peerName, []string{seatName}, nil, "pending-before-rebind")
	db := filepath.Join(bus, "hcom.db")
	unreadBefore := unread(t, bin, bus, seatName)
	cursorBefore, rowsBefore := identityCursorAndRows(t, db, seatName)
	if unreadBefore == 0 || rowsBefore != 1 {
		t.Fatalf("seeded identity unread=%d rows=%d, want pending message on exactly one row", unreadBefore, rowsBefore)
	}
	state := shortState(t)
	b, err := OpenBinder(BinderConfig{Seat: "seat-process", StateDir: state, HcomBin: bin, HcomDir: bus})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = writeAtomic(filepath.Join(SeatDir(state, "seat-process"), "bus-name"), []byte(seatName+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := b.bindIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed != seatName {
		t.Fatalf("reclaimed %q, want %q", reclaimed, seatName)
	}
	got := processBindings(t, db)
	if got["foreign-process"] != foreignName || got["seat-process"] != seatName {
		t.Fatalf("bindings after start=%v, want foreign preserved and seat-owned binding", got)
	}
	unreadAfter := unread(t, bin, bus, seatName)
	cursorAfter, rowsAfter := identityCursorAndRows(t, db, seatName)
	if unreadAfter != unreadBefore || cursorAfter != cursorBefore {
		t.Fatalf("identified stabilization consumed pending state: unread %d -> %d, cursor %d -> %d", unreadBefore, unreadAfter, cursorBefore, cursorAfter)
	}
	if rowsAfter != 1 {
		t.Fatalf("seat roster rows=%d after bind, want exactly one", rowsAfter)
	}
	reclaimed, err = b.bindIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed != seatName {
		t.Fatalf("reclaimed %q, want %q", reclaimed, seatName)
	}
	got = processBindings(t, db)
	if got["foreign-process"] != foreignName || got["seat-process"] != seatName {
		t.Fatalf("bindings after reclaim=%v, want both preserved", got)
	}
	if _, rows := identityCursorAndRows(t, db, seatName); rows != 1 {
		t.Fatalf("seat roster rows=%d after reclaim, want exactly one", rows)
	}

	// Age the numeric epoch beyond hcom's 30-second placeholder timeout, then
	// force an observer pass without sleeping. The identified read performed by
	// bindIdentity must keep the row targetable.
	ageIdentityPlaceholder(t, db, seatName)
	_ = hrun(t, bin, bus, "list", "--json")
	hsend(t, bin, bus, peerName, []string{seatName}, nil, "accepted-after-placeholder-timeout")
}

func TestSeatIdentityInvocationUsesControlledAllowlist(t *testing.T) {
	unsetHcomContractIdentityEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("CODEX_THREAD_ID", "foreign-thread")
	t.Setenv("HCOM", "foreign-command")
	t.Setenv("HCOM_DIR", "foreign-bus")
	t.Setenv("HCOM_LAUNCHED", "foreign-launch")
	t.Setenv("HCOM_PROCESS_ID", "foreign-process")
	t.Setenv("HCOM_TAG", "foreign-tag")
	t.Setenv("HCOM_TOOL", "foreign-tool")
	t.Setenv("PARENT_ONLY", "must-not-cross")

	dir := t.TempDir()
	capture := filepath.Join(dir, "identity-env")
	bin := filepath.Join(dir, "hcom")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n/usr/bin/env > \"${0%/*}/identity-env\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	bus := t.TempDir()
	b, err := OpenBinder(BinderConfig{Seat: "seat-process", StateDir: shortState(t), HcomBin: bin, HcomDir: bus})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err = b.runHcomSeatIdentity(context.Background(), "list", "--json"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			got[key] = value
		}
	}
	for key := range got {
		foreign := key == "HCOM" || key == "CODEX_THREAD_ID" || key == "PARENT_ONLY" || strings.HasPrefix(key, "CLAUDE")
		ambientHcom := strings.HasPrefix(key, "HCOM_") && key != "HCOM_DIR" && key != "HCOM_PROCESS_ID" && key != "HCOM_TOOL"
		if foreign || ambientHcom {
			t.Fatalf("parent signal %s crossed identity environment allowlist", key)
		}
	}
	for key, want := range map[string]string{
		"HOME":            home,
		"HCOM_DIR":        bus,
		"HCOM_PROCESS_ID": "seat-process",
		"HCOM_TOOL":       "adhoc",
	} {
		if got[key] != want {
			t.Errorf("identity env %s = %q, want %q", key, got[key], want)
		}
	}
}

func TestOpenBinderPreservesArgv0DispatchShim(t *testing.T) {
	dir := t.TempDir()
	dispatcher := filepath.Join(dir, "dispatcher")
	if err := os.WriteFile(dispatcher, []byte("#!/bin/sh\n[ \"${0##*/}\" = hcom ] || exit 97\nprintf 'shim-ok\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	dispatchLink := filepath.Join(dir, "dispatch-link")
	if err := os.Symlink(dispatcher, dispatchLink); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "hcom")
	if err := os.Symlink(dispatchLink, shim); err != nil {
		t.Fatal(err)
	}

	state := shortState(t)
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: shim})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.cfg.HcomBin != shim {
		t.Fatalf("binder exec path = %q, want invoked shim %q", b.cfg.HcomBin, shim)
	}
	recorded, err := os.ReadFile(filepath.Join(SeatDir(state, "seat"), "hcom-bin"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(recorded)) != shim {
		t.Fatalf("recorded hcom-bin = %q, want invoked shim %q", strings.TrimSpace(string(recorded)), shim)
	}
	if out, err := b.runHcomSeatIdentity(context.Background(), "list", "--json"); err != nil || strings.TrimSpace(out) != "shim-ok" {
		t.Fatalf("exec preserved hcom shim: err=%v output=%q", err, out)
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
