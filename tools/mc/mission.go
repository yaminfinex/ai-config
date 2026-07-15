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
	bin        string
	repo       string // MISSIONS_REPO for the subprocess; empty = inherit
	mu         sync.Mutex
	slugHits   map[string]missionHit
	statusHits map[string]missionStatusHit
	allHit     missionListHit
}

type missionHit struct {
	slug string
	at   time.Time
}

// missionStatus is mc's read-only view of the stable `mish status` JSON
// contract. Keep this local instead of importing mish internals: the CLI is
// the one mission data boundary shared by installed and scratch binaries.
type missionStatus struct {
	OK         bool            `json:"ok"`
	Slug       string          `json:"slug"`
	MissionDir string          `json:"mission_dir"`
	Manifest   missionManifest `json:"manifest"`
	Board      missionBoard    `json:"board"`
	Warnings   []string        `json:"warnings"`
	Refusal    string          `json:"refusal"`
	Reason     string          `json:"reason"`
	Remedy     string          `json:"remedy"`
}

type missionManifest struct {
	Mission   string `json:"mission"`
	Authority string `json:"authority"`
	Owner     string `json:"owner"`
	Status    string `json:"status"`
	Created   string `json:"created"`
}

type missionBoard struct {
	Available bool                 `json:"available"`
	Counts    []missionStatusCount `json:"counts"`
	Total     int                  `json:"total"`
	Tasks     []missionTask        `json:"tasks"`
}

type missionStatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type missionTask struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Status string   `json:"status"`
	Labels []string `json:"labels"`
}

type missionStatusHit struct {
	status missionStatus
	at     time.Time
}

type missionListHit struct {
	statuses []missionStatus
	warning  string
	at       time.Time
}

func newMissionResolver(bin, repo string) *missionResolver {
	return &missionResolver{
		bin:        bin,
		repo:       repo,
		slugHits:   map[string]missionHit{},
		statusHits: map[string]missionStatusHit{},
	}
}

func (m *missionResolver) Status(slug string) missionStatus {
	if m == nil || m.bin == "" {
		return missionStatus{Reason: "mission status is unavailable: mish is disabled"}
	}
	m.mu.Lock()
	if h, ok := m.statusHits[slug]; ok && time.Since(h.at) < time.Minute {
		m.mu.Unlock()
		return h.status
	}
	m.mu.Unlock()

	var status missionStatus
	out, runErr := m.run("status", "--mission", slug)
	if err := json.Unmarshal(bytes.TrimSpace(out), &status); err != nil {
		status.Reason = "mish status returned unreadable JSON"
		if runErr != nil {
			status.Reason = "mish status failed: " + runErr.Error()
		}
	}
	m.mu.Lock()
	m.statusHits[slug] = missionStatusHit{status: status, at: time.Now()}
	m.mu.Unlock()
	return status
}

func (m *missionResolver) AllStatuses() ([]missionStatus, string) {
	if m == nil || m.bin == "" {
		return nil, "mission list is unavailable: mish is disabled"
	}
	m.mu.Lock()
	if time.Since(m.allHit.at) < time.Minute {
		h := m.allHit
		m.mu.Unlock()
		return h.statuses, h.warning
	}
	m.mu.Unlock()

	var statuses []missionStatus
	out, runErr := m.run("status", "--all")
	warning := ""
	if err := json.Unmarshal(bytes.TrimSpace(out), &statuses); err != nil {
		var refusal missionStatus
		if objectErr := json.Unmarshal(bytes.TrimSpace(out), &refusal); objectErr == nil && !refusal.OK && refusal.Refusal != "" {
			warning = formatMissionRefusal(refusal)
		} else {
			warning = "mish status --all returned unreadable JSON"
			if runErr != nil {
				warning = "mish status --all failed: " + runErr.Error()
			}
		}
	}
	m.mu.Lock()
	m.allHit = missionListHit{statuses: statuses, warning: warning, at: time.Now()}
	m.mu.Unlock()
	return statuses, warning
}

func formatMissionRefusal(refusal missionStatus) string {
	warning := "mission list unavailable (" + refusal.Refusal + ")"
	if refusal.Reason != "" {
		warning += ": " + refusal.Reason
	}
	if refusal.Remedy != "" {
		warning += " — " + refusal.Remedy
	}
	return warning
}

func (s missionStatus) CardWarning() string {
	if s.Reason != "" {
		return s.Reason
	}
	if len(s.Warnings) > 0 {
		return s.Warnings[0]
	}
	return "mission status unavailable"
}

func (m *missionResolver) run(args ...string) ([]byte, error) {
	cmd := exec.Command(m.bin, args...)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "MISSIONS_REPO=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	if m.repo != "" {
		cmd.Env = append(cmd.Env, "MISSIONS_REPO="+m.repo)
	}
	return cmd.Output()
}

func (m *missionResolver) Slug(dir string) string {
	if m == nil || m.bin == "" || dir == "" {
		return ""
	}
	m.mu.Lock()
	if h, ok := m.slugHits[dir]; ok && time.Since(h.at) < 5*time.Minute {
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
	m.slugHits[dir] = missionHit{slug: slug, at: time.Now()}
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
