package grokbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
	t.Skip("real hcom binary unavailable; set HERDER_TEST_HCOM_BIN")
	return ""
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
	out := hrun(t, bin, dir, "start")
	m := regexp.MustCompile(`(?m)^\[hcom:([A-Za-z0-9-]+)\]`).FindStringSubmatch(out)
	if len(m) != 2 {
		t.Fatalf("start output has no name: %s", out)
	}
	return m[1]
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
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: t.TempDir(), HcomBin: bin, HcomDir: bus, BusName: seat, Wait: time.Second})
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
	state := t.TempDir()
	b, err := OpenBinder(BinderConfig{Seat: "seat", StateDir: state, HcomBin: bin, HcomDir: bus, BusName: seat})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := b.events(context.Background(), false, 0)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	for _, ev := range rows[:7] {
		raw, _ := eventRaw(ev)
		if _, _, err = b.journal.Queue(raw); err != nil {
			t.Fatal(err)
		}
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
	p, _ := b.journal.Pending(b.generation, false)
	if len(p) != 27 {
		t.Fatalf("journaled %d, want 27 (prefix cursor %d)", len(p), prefix)
	}
	ids := make([]int64, len(p))
	seen := map[string]bool{}
	for i, r := range p {
		ids[i] = r.Event.ID
		if seen[r.Message.Text] {
			t.Fatalf("duplicate payload %q", r.Message.Text)
		}
		seen[r.Message.Text] = true
	}
	if !sort.SliceIsSorted(ids, func(i, j int) bool { return ids[i] < ids[j] }) {
		t.Fatalf("ids not ascending: %v", ids)
	}
	for i := 0; i < 27; i++ {
		if !seen["payload-"+fmt.Sprintf("%02d", i)] {
			t.Fatal("missing payload " + strconv.Itoa(i))
		}
	}
}
