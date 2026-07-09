package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mish/internal/missionfs"
)

func TestNewScaffoldsMissionTreeManifestBoardArtifactsMarkerAndEcho(t *testing.T) {
	repo := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "work")
	mkdir(t, cwd)
	d := newTestDeps(repo, cwd)
	d.env = func(key string) string {
		if key == "SESSION_OWNER" {
			return "riley"
		}
		return ""
	}

	var stdout, stderr bytes.Buffer
	d.stdout = &stdout
	d.stderr = &stderr
	code := runWithDeps([]string{"new", "perf-regression", "--authority", "hera"}, d)
	if code != exitOK {
		t.Fatalf("new exit = %d, want 0; stderr=%s", code, stderr.String())
	}

	missionDir := filepath.Join(repo, "missions", "perf-regression")
	assertExactMissionTree(t, missionDir)
	manifest, findings, err := missionfs.ReadManifest(missionDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("manifest findings = %#v, want none", findings)
	}
	wantManifest := missionfs.Manifest{
		Mission:   "perf-regression",
		Authority: "hera",
		Owner:     "riley",
		Status:    "active",
		Created:   "2026-07-09",
	}
	if manifest != wantManifest {
		t.Fatalf("manifest = %#v, want %#v", manifest, wantManifest)
	}
	manifestData := readFile(t, filepath.Join(missionDir, "mission.md"))
	if !strings.Contains(manifestData, "# perf regression\n") {
		t.Fatalf("default title missing from manifest:\n%s", manifestData)
	}

	cfg, boardFindings, err := missionfs.ReadBoardConfig(filepath.Join(missionDir, "backlog"))
	if err != nil {
		t.Fatal(err)
	}
	if len(boardFindings) != 0 {
		t.Fatalf("board findings = %#v, want none", boardFindings)
	}
	if cfg.ProjectName != "perf-regression" {
		t.Fatalf("project_name = %q, want perf-regression", cfg.ProjectName)
	}
	if marker := strings.TrimSpace(readFile(t, filepath.Join(cwd, ".mission"))); marker != "perf-regression" {
		t.Fatalf("marker = %q, want perf-regression", marker)
	}
	out := stdout.String()
	for _, want := range []string{
		"created mission perf-regression",
		"authority: hera (source: flag)",
		"owner: riley (source: env)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestNewUsesOSUserDefaultsAndOwnerFlag(t *testing.T) {
	repo := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "work")
	mkdir(t, cwd)
	d := newTestDeps(repo, cwd)

	var stdout, stderr bytes.Buffer
	d.stdout = &stdout
	d.stderr = &stderr
	code := runWithDeps([]string{"new", "ops-handoff", "--owner", "bigboss", "--title", "Ops Handoff"}, d)
	if code != exitOK {
		t.Fatalf("new exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	manifest, _, err := missionfs.ReadManifest(filepath.Join(repo, "missions", "ops-handoff"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Authority != "tester" || manifest.Owner != "bigboss" {
		t.Fatalf("manifest authority/owner = %q/%q, want tester/bigboss", manifest.Authority, manifest.Owner)
	}
	out := stdout.String()
	for _, want := range []string{
		"authority: tester (source: OS user)",
		"owner: bigboss (source: flag)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
	if data := readFile(t, filepath.Join(repo, "missions", "ops-handoff", "mission.md")); !strings.Contains(data, "# Ops Handoff\n") {
		t.Fatalf("--title not written:\n%s", data)
	}
}

func TestNewRefusals(t *testing.T) {
	t.Run("unset missions repo", func(t *testing.T) {
		d := newTestDeps("", t.TempDir())
		code, _, stderr := runNewForTest([]string{"new", "perf-regression"}, d)
		if code != exitRefuse {
			t.Fatalf("exit = %d, want refusal", code)
		}
		assertContains(t, stderr, "$MISSIONS_REPO is not set")
	})

	t.Run("existing slug", func(t *testing.T) {
		repo := t.TempDir()
		mkdir(t, filepath.Join(repo, "missions", "perf-regression"))
		d := newTestDeps(repo, t.TempDir())
		code, _, stderr := runNewForTest([]string{"new", "perf-regression"}, d)
		if code != exitRefuse {
			t.Fatalf("exit = %d, want refusal", code)
		}
		assertContains(t, stderr, "mission perf-regression already exists")
	})

	for _, slug := range []string{"Perf_Regression", "-x", "a--b", "x-", strings.Repeat("a", 65)} {
		t.Run("invalid "+slug, func(t *testing.T) {
			d := newTestDeps(t.TempDir(), t.TempDir())
			code, _, stderr := runNewForTest([]string{"new", slug}, d)
			if code != exitRefuse {
				t.Fatalf("exit = %d, want refusal", code)
			}
			assertContains(t, stderr, "invalid slug")
		})
	}
}

func TestNewMarkerMatrix(t *testing.T) {
	t.Run("different marker refuses", func(t *testing.T) {
		repo := t.TempDir()
		base := filepath.Join(t.TempDir(), "repo")
		cwd := filepath.Join(base, "nested")
		mkdir(t, cwd)
		writeFileForTest(t, filepath.Join(base, ".mission"), "other\n")
		d := newTestDeps(repo, cwd)

		code, _, stderr := runNewForTest([]string{"new", "perf-regression"}, d)
		if code != exitRefuse {
			t.Fatalf("exit = %d, want refusal", code)
		}
		assertContains(t, stderr, "names other, not perf-regression")
		if _, err := os.Stat(filepath.Join(repo, "missions", "perf-regression")); !os.IsNotExist(err) {
			t.Fatalf("mission dir was created despite marker refusal")
		}
	})

	t.Run("same marker is no-op", func(t *testing.T) {
		repo := t.TempDir()
		cwd := filepath.Join(t.TempDir(), "work")
		mkdir(t, cwd)
		writeFileForTest(t, filepath.Join(cwd, ".mission"), "perf-regression\n")
		d := newTestDeps(repo, cwd)

		code, _, stderr := runNewForTest([]string{"new", "perf-regression"}, d)
		if code != exitOK {
			t.Fatalf("exit = %d, want ok; stderr=%s", code, stderr)
		}
		if got := readFile(t, filepath.Join(cwd, ".mission")); got != "perf-regression\n" {
			t.Fatalf("marker changed = %q", got)
		}
	})

	t.Run("no marker flag skips marker", func(t *testing.T) {
		repo := t.TempDir()
		cwd := filepath.Join(t.TempDir(), "work")
		mkdir(t, cwd)
		d := newTestDeps(repo, cwd)

		code, _, stderr := runNewForTest([]string{"new", "perf-regression", "--no-marker"}, d)
		if code != exitOK {
			t.Fatalf("exit = %d, want ok; stderr=%s", code, stderr)
		}
		assertNotExists(t, filepath.Join(cwd, ".mission"))
	})

	t.Run("inside missions repo skips marker", func(t *testing.T) {
		repo := t.TempDir()
		cwd := filepath.Join(repo, "scratch")
		mkdir(t, cwd)
		d := newTestDeps(repo, cwd)

		code, _, stderr := runNewForTest([]string{"new", "perf-regression"}, d)
		if code != exitOK {
			t.Fatalf("exit = %d, want ok; stderr=%s", code, stderr)
		}
		assertNotExists(t, filepath.Join(cwd, ".mission"))
	})
}

func assertExactMissionTree(t *testing.T, missionDir string) {
	t.Helper()
	var got []string
	err := filepath.WalkDir(missionDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == missionDir {
			return nil
		}
		rel, err := filepath.Rel(missionDir, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel += "/"
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"artifacts/",
		"artifacts/.gitkeep",
		"backlog/",
		"backlog/config.yml",
		"backlog/tasks/",
		"mission.md",
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("mission tree = %#v, want %#v", got, want)
	}
}

func newTestDeps(repo, cwd string) deps {
	d := testDeps()
	d.missionsRepo = repo
	d.cwd = func() (string, error) { return cwd, nil }
	d.clock = func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) }
	d.osUser = "tester"
	d.exec = func(string, []string, string, io.Reader, io.Writer, io.Writer) execResult {
		panic("new must not exec external commands")
	}
	d.git = func([]string, string) ([]byte, error) {
		panic("new must not invoke git")
	}
	return d
}

func runNewForTest(args []string, d deps) (int, string, string) {
	var stdout, stderr bytes.Buffer
	d.stdout = &stdout
	d.stderr = &stderr
	return runWithDeps(args, d), stdout.String(), stderr.String()
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFileForTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("missing %q in:\n%s", want, got)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s exists or stat failed with %v", path, err)
	}
}
