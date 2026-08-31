package servecmd

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomidentity"
	"github.com/fsnotify/fsnotify"
)

func transcriptFixtureLine(row hcomidentity.Row, id, text string) string {
	if row.Tool == "codex" {
		return `{"timestamp":"2026-01-02T03:04:05Z","type":"event_msg","payload":{"type":"user_message","message":"` + text + `","id":"` + id + `"}}`
	}
	sidechain := ""
	if isSubagent(row) {
		sidechain = `,"isSidechain":true,"agentId":"` + row.AgentID + `"`
	}
	return `{"type":"assistant","uuid":"` + id + `"` + sidechain + `,"message":{"content":[{"type":"text","text":"` + text + `"}]}}`
}

func appendTranscript(t *testing.T, path, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(content)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append write=%v close=%v", writeErr, closeErr)
	}
}

func readUntilEvent(t *testing.T, reader *bufio.Reader, want string) string {
	t.Helper()
	for {
		event, data := readEvent(t, reader)
		if event == want {
			return data
		}
	}
}

func fileBackedTranscriptDeps(t *testing.T, row hcomidentity.Row, pathFor func(hcomidentity.Row) string) (dependencies, <-chan claudesession.TailResult) {
	t.Helper()
	deps := fixtureDeps()
	deps.poll = time.Hour
	deps.transcriptSafety = time.Hour
	deps.transcriptWatcher = fsnotify.NewWatcher
	deps.roster = func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{row}, nil }
	deps.entryPath = func(current hcomidentity.Row) (string, error) { return pathFor(current), nil }
	deps.entryEnd = func(current hcomidentity.Row) (int64, error) {
		info, err := os.Stat(pathFor(current))
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	results := make(chan claudesession.TailResult, 16)
	deps.entryTail = func(current hcomidentity.Row, cursor claudesession.Cursor, limit int) (claudesession.TailResult, error) {
		result, err := readEntryWindow(pathFor(current), current, cursor, limit)
		if err == nil {
			results <- result
		}
		return result, err
	}
	return deps, results
}

func TestSessionFileWritesPushClaudeCodexAndSidechainEntries(t *testing.T) {
	tests := []struct {
		name string
		row  hcomidentity.Row
	}{
		{name: "claude", row: hcomidentity.Row{Name: "dore", Tool: "claude", Status: "active", SessionID: "claude-session"}},
		{name: "claude-sidechain", row: hcomidentity.Row{Name: "dore", Tool: "claude", Status: "active", SessionID: "parent-session", ParentAgent: "parent", AgentID: "child-agent"}},
		{name: "codex-rollout", row: hcomidentity.Row{Name: "dore", Tool: "codex", Status: "active", SessionID: "codex-session"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			initial := transcriptFixtureLine(test.row, "initial", "initial") + "\n"
			if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
				t.Fatal(err)
			}
			deps, tailResults := fileBackedTranscriptDeps(t, test.row, func(hcomidentity.Row) string { return path })
			server := httptest.NewServer(newHandler(deps))
			defer server.Close()
			response, err := http.Get(server.URL + "/api/events?agents=dore")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			reader := bufio.NewReader(response.Body)
			if event, _ := readEvent(t, reader); event != "hello" {
				t.Fatalf("hello=%q", event)
			}
			if event, _ := readEvent(t, reader); event != "fleet" {
				t.Fatalf("fleet=%q", event)
			}

			partial := transcriptFixtureLine(test.row, "appended", "appended")
			appendTranscript(t, path, partial)
			select {
			case result := <-tailResults:
				if len(result.Read.Entries) != 0 || result.Cursor.Offset != int64(len(initial)) {
					t.Fatalf("partial tail=%+v", result)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("partial write did not wake transcript tail")
			}
			appendTranscript(t, path, "\n")
			data := readUntilEvent(t, reader, "entry:dore")
			if !strings.Contains(data, "appended") {
				t.Fatalf("entry data=%s", data)
			}
			select {
			case result := <-tailResults:
				if len(result.Read.Entries) != 1 {
					t.Fatalf("completed partial tail=%+v", result)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("completed partial did not produce one parsed entry")
			}

			replacement := filepath.Join(filepath.Dir(path), "replacement.jsonl")
			if err := os.WriteFile(replacement, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replacement, path); err != nil {
				t.Fatal(err)
			}
			reset := readUntilEvent(t, reader, "rewindow")
			if reset != `{"agent":"dore"}` {
				t.Fatalf("rewindow=%s", reset)
			}
			appendTranscript(t, path, transcriptFixtureLine(test.row, "after-replace", "after replace")+"\n")
			if data := readUntilEvent(t, reader, "entry:dore"); !strings.Contains(data, "after replace") {
				t.Fatalf("replacement entry=%s", data)
			}
		})
	}
}

func TestSessionResumeRebuildsWatchedPathAndPreservesRewindow(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.jsonl")
	secondPath := filepath.Join(root, "second.jsonl")
	first := hcomidentity.Row{Name: "dore", Tool: "claude", Status: "listening", SessionID: "first"}
	second := hcomidentity.Row{Name: "dore", Tool: "claude", Status: "active", SessionID: "second"}
	for path, line := range map[string]string{
		firstPath:  transcriptFixtureLine(first, "first", "first") + "\n",
		secondPath: transcriptFixtureLine(second, "second", "second") + "\n",
	} {
		if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths := map[string]string{"first": firstPath, "second": secondPath}
	var current atomic.Value
	current.Store(first)
	deps, _ := fileBackedTranscriptDeps(t, first, func(row hcomidentity.Row) string { return paths[row.SessionID] })
	deps.poll = 10 * time.Millisecond
	deps.roster = func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{current.Load().(hcomidentity.Row)}, nil }
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events?agents=dore")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	readUntilEvent(t, reader, "fleet")

	current.Store(second)
	if reset := readUntilEvent(t, reader, "rewindow"); reset != `{"agent":"dore"}` {
		t.Fatalf("resume rewindow=%s", reset)
	}
	appendTranscript(t, secondPath, transcriptFixtureLine(second, "resumed", "resumed")+"\n")
	data := readUntilEvent(t, reader, "entry:dore")
	var entry entryResponse
	if err := json.Unmarshal([]byte(data), &entry); err != nil || !strings.Contains(string(entry.Payload), "resumed") {
		t.Fatalf("resume entry=%s err=%v", data, err)
	}
}
