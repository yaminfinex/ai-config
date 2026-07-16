package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strconv"
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
	bin           string
	repo          string // MISSIONS_REPO for the subprocess; empty = inherit
	mu            sync.Mutex
	slugHits      map[string]missionHit
	statusHits    map[string]missionStatusHit
	allHit        missionListHit
	milestoneHits map[string]milestoneHit
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
	Asks       missionAsks     `json:"asks"`
	Warnings   []string        `json:"warnings"`
	Refusal    string          `json:"refusal"`
	Reason     string          `json:"reason"`
	Remedy     string          `json:"remedy"`
	FetchedAt  time.Time       `json:"-"`
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
		bin:           bin,
		repo:          repo,
		slugHits:      map[string]missionHit{},
		statusHits:    map[string]missionStatusHit{},
		milestoneHits: map[string]milestoneHit{},
	}
}

// missionMilestone is one row of the crew-authored narrative layer, read
// through `mish backlog milestone list` — the one board read boundary.
type missionMilestone struct {
	ID        string
	Title     string
	Done      int
	Total     int
	Completed bool
}

type milestoneHit struct {
	milestones []missionMilestone
	at         time.Time
}

func (m *missionResolver) Milestones(slug string) []missionMilestone {
	if m == nil || m.bin == "" || slug == "" {
		return nil
	}
	m.mu.Lock()
	if h, ok := m.milestoneHits[slug]; ok && time.Since(h.at) < time.Minute {
		m.mu.Unlock()
		return h.milestones
	}
	m.mu.Unlock()

	out, _ := m.run("backlog", "--mission", slug, "milestone", "list", "--plain", "--show-completed")
	milestones := parseMilestones(string(out))
	m.mu.Lock()
	m.milestoneHits[slug] = milestoneHit{milestones: milestones, at: time.Now()}
	m.mu.Unlock()
	return milestones
}

var milestoneLine = regexp.MustCompile(`^\s+([A-Za-z0-9_.-]+): (.+) \((\d+)/(\d+) done\)$`)

// parseMilestones reads Backlog.md's plain milestone listing:
//
//	Active milestones (1):
//	  m-1: phase two: build (0/1 done)
//	Completed milestones (1):
//	  m-0: phase one: capture (1/1 done)
func parseMilestones(text string) []missionMilestone {
	completed := false
	var out []missionMilestone
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Active milestones") {
			completed = false
			continue
		}
		if strings.HasPrefix(trimmed, "Completed milestones") {
			completed = true
			continue
		}
		match := milestoneLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		done, _ := strconv.Atoi(match[3])
		total, _ := strconv.Atoi(match[4])
		out = append(out, missionMilestone{ID: match[1], Title: match[2], Done: done, Total: total, Completed: completed})
	}
	return out
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
	status.FetchedAt = time.Now()
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
	fetchedAt := time.Now()
	for i := range statuses {
		statuses[i].FetchedAt = fetchedAt
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
