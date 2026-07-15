package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// missionResolver maps directories to mission slugs by shelling out to
// `mish resolve` — mish is the ONE resolution point (owner ruling 07-15);
// mc re-implements no marker walking. Refusals (exit 1) still print the
// JSON line, so output is parsed regardless of exit code, and any failure
// just means "no mission" — grouping falls back gracefully.

type missionResolver struct {
	bin  string
	repo string // MISSIONS_REPO for the subprocess; empty = inherit
	mu   sync.Mutex
	hit  map[string]missionHit
}

type missionHit struct {
	slug string
	at   time.Time
}

func newMissionResolver(bin, repo string) *missionResolver {
	return &missionResolver{bin: bin, repo: repo, hit: map[string]missionHit{}}
}

func (m *missionResolver) Slug(dir string) string {
	if m == nil || m.bin == "" || dir == "" {
		return ""
	}
	m.mu.Lock()
	if h, ok := m.hit[dir]; ok && time.Since(h.at) < 5*time.Minute {
		m.mu.Unlock()
		return h.slug
	}
	m.mu.Unlock()

	cmd := exec.Command(m.bin, "resolve")
	cmd.Dir = dir
	if m.repo != "" {
		cmd.Env = append(os.Environ(), "MISSIONS_REPO="+m.repo)
	}
	out, _ := cmd.Output()
	var r struct {
		OK   bool   `json:"ok"`
		Slug string `json:"slug"`
	}
	slug := ""
	if json.Unmarshal(bytes.TrimSpace(out), &r) == nil && r.OK {
		slug = r.Slug
	}
	m.mu.Lock()
	m.hit[dir] = missionHit{slug: slug, at: time.Now()}
	m.mu.Unlock()
	return slug
}

// groupKey names the roster group for a directory: mission slug when one
// resolves, collapsed worktree identity next, raw path last.
func (m *missionResolver) groupKey(dir string) string {
	if dir == "" {
		return "(no directory)"
	}
	if slug := m.Slug(dir); slug != "" {
		return "mission: " + slug
	}
	home, err := os.UserHomeDir()
	if err == nil {
		wt := filepath.Join(home, ".herdr", "worktrees") + string(filepath.Separator)
		if rest, ok := strings.CutPrefix(dir, wt); ok {
			parts := strings.SplitN(rest, string(filepath.Separator), 3)
			if len(parts) >= 2 {
				return "worktree: " + parts[0] + " @ " + parts[1]
			}
		}
	}
	return dir
}

// groupRank orders roster sections: missions, then worktrees, then paths.
func groupRank(key string) int {
	switch {
	case strings.HasPrefix(key, "mission: "):
		return 0
	case strings.HasPrefix(key, "worktree: "):
		return 1
	default:
		return 2
	}
}
