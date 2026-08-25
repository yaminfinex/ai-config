package missioncontext

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCWDMatchesMissionAndMarkerPrecedence(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	missionDir := filepath.Join(repo, "missions", "cwd-mission")
	cwd := filepath.Join(missionDir, "nested")
	for _, dir := range []string{cwd, filepath.Join(repo, "missions", "marker-mission")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(missionDir, "mission.md"), []byte("mission: cwd-mission\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missionDir, ".mission"), []byte("marker-mission\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mission, err := ResolveCWD(Options{CWD: cwd, Env: func(string) string { return repo }})
	if err != nil {
		t.Fatal(err)
	}
	if mission.Slug != "cwd-mission" || mission.Source != SourceCWD {
		t.Fatalf("mission = %+v", mission)
	}
}
