package missioncontext

import (
	"errors"
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

func TestResolveExplicitTypedRefusals(t *testing.T) {
	tests := []struct {
		name string
		slug string
		env  func(string) string
		kind string
	}{
		{name: "invalid", slug: "Bad--slug", env: os.Getenv, kind: "invalid_mission_slug"},
		{name: "repo unset", slug: "valid", env: func(string) string { return "" }, kind: "missions_repo_unset"},
		{name: "missing", slug: "valid", env: func(string) string { return t.TempDir() }, kind: "mission_not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveExplicit(tt.slug, Options{Env: tt.env})
			var refusal *Refusal
			if !errors.As(err, &refusal) || refusal.Kind != tt.kind || refusal.Remedy == "" {
				t.Fatalf("err = %#v, want %s refusal with remedy", err, tt.kind)
			}
		})
	}
}
