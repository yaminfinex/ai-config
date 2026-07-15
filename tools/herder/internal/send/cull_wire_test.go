package send

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type taggedWireRow struct {
	Name     string `json:"name"`
	BaseName string `json:"base_name"`
}

func TestCullTaggedWireAttribution(t *testing.T) {
	if testing.Short() {
		t.Skip("real hcom contract")
	}
	bin := installedCullHcom(t)
	root := t.TempDir()
	for _, dir := range []string{"home", "bus", "state"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("HCOM_DIR", filepath.Join(root, "bus"))
	t.Setenv("HERDER_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("HCOM_TAG", "wire")
	for _, key := range []string{"HCOM_SESSION_ID", "HCOM_PROCESS_ID", "HCOM_INSTANCE_NAME", "HCOM_BASE_NAME", "CODEX_THREAD_ID"} {
		t.Setenv(key, "")
	}

	runCullHcom(t, bin, "start", "--as", "callr")
	runCullHcom(t, bin, "start", "--as", "targt")
	var rows []taggedWireRow
	if err := json.Unmarshal([]byte(runCullHcom(t, bin, "list", "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	caller := findTaggedWireRow(t, rows, "callr")
	target := findTaggedWireRow(t, rows, "targt")
	if caller.Name == caller.BaseName || target.Name == target.BaseName {
		t.Fatalf("wire fixture is not tagged: caller=%+v target=%+v", caller, target)
	}

	targetReadDone := make(chan error, 1)
	go func() {
		readDeadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(readDeadline) {
			probe := exec.Command(bin, "events", "--full", "--last", "20", "--from", caller.BaseName, "--thread", "task234-tagged-wire")
			probe.Env = os.Environ()
			out, err := probe.CombinedOutput()
			if err == nil && strings.Contains(string(out), `"intent":"request"`) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !time.Now().Before(readDeadline) {
			targetReadDone <- fmt.Errorf("tagged notice did not appear before target read deadline")
			return
		}
		cmd := exec.Command(bin, "events", "--all", "--name", target.Name)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			err = fmt.Errorf("target read: %w: %s", err, out)
		}
		targetReadDone <- err
	}()

	req := CullRequest{
		Sender: caller.Name, SenderBase: caller.BaseName,
		Target: target.Name, TargetBase: target.BaseName,
		BusDir: os.Getenv("HCOM_DIR"), Thread: "task234-tagged-wire",
		Message: "release external resources, then acknowledge", Deadline: time.Now().Add(3 * time.Second),
	}
	delivery := DeliverCullRequest(req)
	if err := <-targetReadDone; err != nil {
		t.Fatal(err)
	}
	if delivery.Verdict != "delivered" {
		t.Fatalf("delivery=%+v, want full-name delivery receipt recognition", delivery)
	}
	if delivery.NoticeID == 0 {
		t.Fatalf("delivery=%+v, want base-stamped notice anchor", delivery)
	}

	runCullHcom(t, bin, "send", "--name", target.Name, "@"+caller.Name,
		"--intent", "ack", "--reply-to", strconv.FormatInt(delivery.NoticeID, 10),
		"--thread", req.Thread, "--", "released")
	acked, noticeID := CullAckObserved(req, delivery)
	if !acked || noticeID != delivery.NoticeID {
		t.Fatalf("CullAckObserved = (%v, %d), want tagged target base-name ack for notice %d", acked, noticeID, delivery.NoticeID)
	}
}

func installedCullHcom(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("HERDER_TEST_HCOM_BIN"); path != "" {
		return path
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		path := filepath.Join(dir, "hcom")
		if strings.Contains(path, "tools/herder/shims") {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.Mode()&0o111 != 0 {
			return path
		}
	}
	t.Fatal("real hcom binary unavailable; install hcom 0.7.23 or set HERDER_TEST_HCOM_BIN")
	return ""
}

func runCullHcom(t *testing.T, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hcom %v: %v: %s", args, err, out)
	}
	return string(out)
}

func findTaggedWireRow(t *testing.T, rows []taggedWireRow, base string) taggedWireRow {
	t.Helper()
	for _, row := range rows {
		if row.BaseName == base {
			return row
		}
	}
	t.Fatalf("base name %q absent from tagged roster: %+v", base, rows)
	return taggedWireRow{}
}
