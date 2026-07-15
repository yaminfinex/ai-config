package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissionFilePathRefusesTraversalAndSymlinkEscapes(t *testing.T) {
	root := t.TempDir()
	artifacts := filepath.Join(root, "artifacts")
	if err := os.Mkdir(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("outside secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(artifacts, "file-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(artifacts, "dir-link")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		rel  string
		kind string
	}{
		{"parent traversal", "../secret.txt", "path_escape"},
		{"nested parent traversal", "artifacts/../../secret.txt", "path_escape"},
		{"absolute path", outside, "path_escape"},
		{"empty path", "", "path_escape"},
		{"root file out of scope", "Backlog.md", "file_out_of_scope"},
		{"file symlink escape", "artifacts/file-link", "path_escape"},
		{"directory symlink escape", "artifacts/dir-link/secret.txt", "path_escape"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := missionFilePath(root, tc.rel)
			var refusal *fileRefusal
			if err == nil || !errors.As(err, &refusal) {
				t.Fatalf("missionFilePath(%q) error = %v, want typed refusal", tc.rel, err)
			}
			if refusal.kind != tc.kind {
				t.Fatalf("missionFilePath(%q) refusal = %q, want %q", tc.rel, refusal.kind, tc.kind)
			}
		})
	}
}

func TestMissionArtifactPageAndViewer(t *testing.T) {
	missionDir := t.TempDir()
	artifacts := filepath.Join(missionDir, "artifacts", "design docs")
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missionDir, "mission.md"), []byte("# Mission\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	markdown := `# Design

` + "```text\n+-----+\n| <safe> |\n+-----+\n```" + `

<script>alert('no')</script>

Allowed: <https://example.com>, <http://example.com>, <friend@example.com>, [relative](../next).

Refused: <javascript:alert(1)>, <vbscript:msgbox(1)>, <data:text/html;base64,PHNjcmlwdD4=>, [ftp](ftp://example.com).
`
	if err := os.WriteFile(filepath.Join(artifacts, "wire frame.md"), []byte(markdown), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missionDir, "artifacts", "decimal-entity.md"), []byte("[poison](&#106;avascript:alert(1))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missionDir, "artifacts", "hex-entity.md"), []byte("[poison](&#x6a;avascript:alert(1))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missionDir, "artifacts", "raw.txt"), []byte("<script>not executable</script>"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("must not appear"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(missionDir, "artifacts", "escape.txt")); err != nil {
		t.Fatal(err)
	}

	w := artifactTestWeb(t, missionDir)
	tests := []struct {
		path       string
		status     int
		contains   []string
		notContain []string
	}{
		{
			path:   "/mission/mission-one",
			status: http.StatusOK,
			contains: []string{"Artifacts", "mission.md", "artifacts/design docs/wire frame.md", "artifacts/raw.txt",
				`/mission/mission-one/file/artifacts/design%20docs/wire%20frame.md`},
			notContain: []string{"escape.txt", "<script"},
		},
		{
			path:   "/mission/mission-one/file/artifacts/design%20docs/wire%20frame.md",
			status: http.StatusOK,
			contains: []string{"<h1>Design</h1>", "<pre><code", "+-----+", "&lt;safe&gt;",
				`href="https://example.com"`, `href="http://example.com"`,
				`href="mailto:friend@example.com"`, `href="../next"`},
			notContain: []string{"<script", "alert('no')", `href="javascript:`,
				`href="vbscript:`, `href="data:`, `href="ftp:`},
		},
		{
			path:       "/mission/mission-one/file/artifacts/decimal-entity.md",
			status:     http.StatusOK,
			notContain: []string{`href="javascript:`},
		},
		{
			path:       "/mission/mission-one/file/artifacts/hex-entity.md",
			status:     http.StatusOK,
			notContain: []string{`href="javascript:`},
		},
		{
			path:       "/mission/mission-one/file/artifacts/raw.txt",
			status:     http.StatusOK,
			contains:   []string{"&lt;script&gt;not executable&lt;/script&gt;"},
			notContain: []string{"<script"},
		},
		{
			path:     "/mission/mission-one/file/artifacts/missing.md",
			status:   http.StatusNotFound,
			contains: []string{"file_not_found", "mission file was not found"},
		},
		{
			path:       "/mission/mission-one/file/artifacts/escape.txt",
			status:     http.StatusBadRequest,
			contains:   []string{"path_escape", "symlink target escapes"},
			notContain: []string{"must not appear"},
		},
		{
			path:     "/mission/mission-one/file/artifacts/%2e%2e/%2e%2e/secret.txt",
			status:   http.StatusBadRequest,
			contains: []string{"path_escape", "escapes the mission folder"},
		},
		{
			path:     "/mission/mission-one/file/%2Fetc%2Fpasswd",
			status:   http.StatusBadRequest,
			contains: []string{"path_escape", "absolute"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			rw := httptest.NewRecorder()
			w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, tc.path, nil))
			const wantCSP = "script-src 'self'; object-src 'none'; base-uri 'none'"
			if got := rw.Header().Get("Content-Security-Policy"); got != wantCSP {
				t.Errorf("GET %s Content-Security-Policy = %q, want %q", tc.path, got, wantCSP)
			}
			if rw.Code != tc.status {
				t.Fatalf("GET %s = %d, want %d: %s", tc.path, rw.Code, tc.status, rw.Body.String())
			}
			body := rw.Body.String()
			bodyWithoutProgressiveHook := strings.Replace(body, `<script src="/mc.js" defer></script>`, "", 1)
			for _, want := range tc.contains {
				if !strings.Contains(body, want) {
					t.Errorf("GET %s missing %q: %s", tc.path, want, body)
				}
			}
			for _, unwanted := range tc.notContain {
				if strings.Contains(bodyWithoutProgressiveHook, unwanted) {
					t.Errorf("GET %s unexpectedly contains %q: %s", tc.path, unwanted, body)
				}
			}
		})
	}
}

func TestMissionViewerDegradesWhenMissionFolderIsUnreadable(t *testing.T) {
	w := artifactTestWeb(t, filepath.Join(t.TempDir(), "missing"))
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/mission/mission-one/file/mission.md", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("missing mission folder status = %d, want 200", rw.Code)
	}
	for _, want := range []string{"mission_unreadable", "mission folder is missing or unreadable"} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Errorf("degraded page missing %q: %s", want, rw.Body.String())
		}
	}
}

func artifactTestWeb(t *testing.T, missionDir string) *Web {
	t.Helper()
	dir := t.TempDir()
	status, err := json.Marshal(missionStatus{
		OK:         true,
		Slug:       "mission-one",
		MissionDir: missionDir,
		Manifest:   missionManifest{Mission: "mission-one"},
		Board:      missionBoard{Available: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf("#!/bin/sh\ncat <<'EOF'\n%s\nEOF\n", status))
	store, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return NewWeb(store, &Bus{}, nil, "human-yamen", "owner", "", newMissionResolver(mish, ""))
}
