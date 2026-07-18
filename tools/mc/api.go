package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// /api/v1 is the JSON face of the same structs the HTML pages render.
// DTOs are explicit wire contracts with tools/mc/ui: internal store types
// never cross wholesale, so store refactors cannot silently change the API.
//
// Truth-forms law (vocabulary doctrine): every derived fact crosses the wire
// naming its system of record and observation time. That stamp — provenance —
// is ALSO the invalidation contract: each stamp carries an opaque version
// token derived from the observed content, and /api/v1/version serves just
// the stamps. A client polls /api/v1/version and refetches a source family
// when its token moves; any data response can be correlated with the poll
// because it carries the same stamp shape. Degraded upstream reads
// (mish/hcom/herder unavailable) stay HTTP 200 with their warnings in the
// payload — the UI renders honesty, not a blank error.
//
// The three source families v1 serves:
//   - journal: mc's own thread journal (in-memory projection; version is the
//     store's cursor-generation pair, observed at request time).
//   - missions: `mish status` through the resolver's cache (observedAt is
//     cache-fill time — an old observation never renders as current). The
//     family has two scopes with different powers: --all for the list, and
//     --mission <slug> for a detail page (see apiVersionDTO).
//   - roster: `hcom list` + `herder list`, read live per request — the same
//     cost every legacy HTML page load already pays.

type apiProvenanceDTO struct {
	Source     string `json:"source"`
	ObservedAt string `json:"observedAt"`
	Version    string `json:"version"`
}

// apiVersionDTO is THE pollable invalidation contract. Its stamps are
// derived by the same functions that stamp data-response sections, so a
// polled token and the matching section token can never drift.
//
// Scope matters inside the missions family: the per-slug projection
// (`mish status --mission`) observes things the --all projection never does
// (git staleness, for one), so the list token cannot vouch for a detail
// payload. A client on the missions list polls bare /api/v1/version and
// watches Missions; a client on a mission-detail page polls
// /api/v1/version?mission=<slug> and watches Mission (plus Journal and
// Roster, which stamp the detail's other two sections). The per-slug stamp
// rides the resolver's per-slug cache — polling never fans out git checks
// across all missions.
type apiVersionDTO struct {
	Journal  apiProvenanceDTO `json:"journal"`
	Missions apiProvenanceDTO `json:"missions"`
	Roster   apiProvenanceDTO `json:"roster"`
	// Mission is the per-slug missions-family stamp, present only when the
	// poll asks ?mission=<slug>; equal by construction to the mission-detail
	// response's mission-section token for the same slug.
	Mission *apiProvenanceDTO `json:"mission,omitempty"`
}

type apiTaskCountDTO struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type apiMissionDTO struct {
	Slug           string            `json:"slug"`
	OK             bool              `json:"ok"`
	Name           string            `json:"name"`
	Owner          string            `json:"owner"`
	Authority      string            `json:"authority"`
	Status         string            `json:"status"`
	Created        string            `json:"created"`
	BoardAvailable bool              `json:"boardAvailable"`
	TaskTotal      int               `json:"taskTotal"`
	TaskCounts     []apiTaskCountDTO `json:"taskCounts"`
	Warnings       []string          `json:"warnings"`
}

type apiMissionsDTO struct {
	Missions   []apiMissionDTO  `json:"missions"`
	Warning    string           `json:"warning"`
	Provenance apiProvenanceDTO `json:"provenance"`
}

type apiThreadDTO struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Grade        string   `json:"grade"`
	Expects      string   `json:"expects"`
	OpenedBy     string   `json:"openedBy"`
	With         []string `json:"with"`
	Turn         string   `json:"turn"`
	Updated      string   `json:"updated"`
	MessageCount int      `json:"messageCount"`
	LastFrom     string   `json:"lastFrom"`
	LastText     string   `json:"lastText"`
}

type apiRosterAgentDTO struct {
	Name          string `json:"name"`
	Address       string `json:"address"`
	Tool          string `json:"tool"`
	Status        string `json:"status"`
	Detail        string `json:"detail"`
	Unread        int    `json:"unread"`
	Role          string `json:"role"`
	Branch        string `json:"branch"`
	MissionSource string `json:"missionSource"`
	Unmanaged     bool   `json:"unmanaged"`
}

// Mission detail is three source-backed sections; provenance sits on the
// section it stamps, never response-global, because the sections come from
// different systems of record observed at different times.

type apiMissionSectionDTO struct {
	Status     apiMissionDTO    `json:"status"`
	Provenance apiProvenanceDTO `json:"provenance"`
}

type apiThreadsSectionDTO struct {
	Rows       []apiThreadDTO   `json:"rows"`
	Provenance apiProvenanceDTO `json:"provenance"`
}

type apiRosterSectionDTO struct {
	Agents     []apiRosterAgentDTO `json:"agents"`
	Warning    string              `json:"warning"`
	Provenance apiProvenanceDTO    `json:"provenance"`
}

type apiMissionDetailDTO struct {
	Mission apiMissionSectionDTO `json:"mission"`
	Threads apiThreadsSectionDTO `json:"threads"`
	Roster  apiRosterSectionDTO  `json:"roster"`
}

const (
	sourceJournal  = "mc journal"
	sourceMissions = "mish status"
	sourceRoster   = "hcom list + herder list"
)

// contentToken derives an opaque change token from observed content.
// Observation stamps must not feed it (missionStatus.FetchedAt is json:"-",
// so a cache refresh that observes identical content keeps the same token).
func contentToken(parts ...any) string {
	h := sha256.New()
	enc := json.NewEncoder(h)
	for _, part := range parts {
		if err := enc.Encode(part); err != nil {
			// Wire DTO inputs are all marshalable; refusing loudly beats a
			// token that silently stopped moving.
			panic(fmt.Sprintf("contentToken: %v", err))
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

func provenance(source string, observedAt time.Time, version string) apiProvenanceDTO {
	return apiProvenanceDTO{
		Source:     source,
		ObservedAt: observedAt.UTC().Format(time.RFC3339),
		Version:    version,
	}
}

func (w *Web) journalProvenance() apiProvenanceDTO {
	cursor, generation := w.store.Version()
	return provenance(sourceJournal, w.now(), fmt.Sprintf("%d-%d", cursor, generation))
}

func missionsProvenance(statuses []missionStatus, warning string, observedAt time.Time) apiProvenanceDTO {
	return provenance(sourceMissions, observedAt, contentToken(statuses, warning))
}

func (w *Web) rosterProvenance(groups []rosterGroup, warning string) apiProvenanceDTO {
	return provenance(sourceRoster, w.now(), contentToken(groups, warning))
}

// missionStatusStamped resolves one slug's status plus the stamp both the
// version poll and the detail response derive from it — one derivation,
// two consumers, zero drift.
func (w *Web) missionStatusStamped(slug string) (missionStatus, apiProvenanceDTO) {
	status := w.missions.Status(slug)
	if status.Slug == "" {
		status.Slug = slug
	}
	observedAt := status.FetchedAt
	if observedAt.IsZero() {
		observedAt = w.now() // disabled resolver: the refusal was observed now
	}
	return status, provenance(sourceMissions, observedAt, contentToken(status))
}

func writeJSON(rw http.ResponseWriter, code int, v any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	rw.WriteHeader(code)
	if err := json.NewEncoder(rw).Encode(v); err != nil {
		log.Printf("api encode: %v", err)
	}
}

func (w *Web) apiVersion(rw http.ResponseWriter, r *http.Request) {
	statuses, missionsWarning, observedAt := w.missions.AllStatuses()
	groups, rosterWarning := w.rosterGroups(false)
	out := apiVersionDTO{
		Journal:  w.journalProvenance(),
		Missions: missionsProvenance(statuses, missionsWarning, observedAt),
		Roster:   w.rosterProvenance(groups, rosterWarning),
	}
	if slug := r.URL.Query().Get("mission"); slug != "" {
		_, stamp := w.missionStatusStamped(slug)
		out.Mission = &stamp
	}
	writeJSON(rw, http.StatusOK, out)
}

func missionDTO(s missionStatus) apiMissionDTO {
	dto := apiMissionDTO{
		Slug:           s.Slug,
		OK:             s.OK,
		Name:           s.Manifest.Mission,
		Owner:          s.Manifest.Owner,
		Authority:      s.Manifest.Authority,
		Status:         s.Manifest.Status,
		Created:        s.Manifest.Created,
		BoardAvailable: s.Board.Available,
		TaskTotal:      s.Board.Total,
		TaskCounts:     []apiTaskCountDTO{},
		Warnings:       append([]string{}, s.Warnings...),
	}
	for _, c := range s.Board.Counts {
		dto.TaskCounts = append(dto.TaskCounts, apiTaskCountDTO{Status: c.Status, Count: c.Count})
	}
	// Typed refusals and transport failures surface as one honest warning
	// line, same as the HTML mission page's degraded rendering.
	if !s.OK && len(dto.Warnings) == 0 {
		dto.Warnings = append(dto.Warnings, s.CardWarning())
	}
	return dto
}

func threadDTO(t *Thread) apiThreadDTO {
	dto := apiThreadDTO{
		ID:           t.ID,
		Title:        t.Title,
		Status:       t.Status,
		Grade:        t.Grade,
		Expects:      t.Expects,
		OpenedBy:     t.OpenedBy,
		With:         append([]string{}, t.With...),
		Turn:         t.Turn,
		Updated:      t.Updated.UTC().Format(time.RFC3339),
		MessageCount: len(t.Msgs),
	}
	if len(t.Msgs) > 0 {
		last := t.Msgs[len(t.Msgs)-1]
		dto.LastFrom = last.From
		dto.LastText = last.Text
	}
	return dto
}

func rosterAgentDTO(a rosterAgent) apiRosterAgentDTO {
	return apiRosterAgentDTO{
		Name:          a.Name,
		Address:       a.Address,
		Tool:          a.Tool,
		Status:        a.Status,
		Detail:        a.Detail,
		Unread:        a.Unread,
		Role:          a.Role,
		Branch:        a.Branch,
		MissionSource: a.MissionSource,
		Unmanaged:     a.Unmanaged,
	}
}

func (w *Web) apiMissions(rw http.ResponseWriter, _ *http.Request) {
	statuses, warning, observedAt := w.missions.AllStatuses()
	out := apiMissionsDTO{
		Missions:   []apiMissionDTO{},
		Warning:    warning,
		Provenance: missionsProvenance(statuses, warning, observedAt),
	}
	for _, s := range statuses {
		out.Missions = append(out.Missions, missionDTO(s))
	}
	writeJSON(rw, http.StatusOK, out)
}

func (w *Web) apiMission(rw http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	status, missionStamp := w.missionStatusStamped(slug)
	out := apiMissionDetailDTO{
		Mission: apiMissionSectionDTO{
			Status:     missionDTO(status),
			Provenance: missionStamp,
		},
		Threads: apiThreadsSectionDTO{
			Rows:       []apiThreadDTO{},
			Provenance: w.journalProvenance(),
		},
		Roster: apiRosterSectionDTO{
			Agents: []apiRosterAgentDTO{},
		},
	}
	for _, t := range w.store.List("", "") {
		if t.Home == slug {
			out.Threads.Rows = append(out.Threads.Rows, threadDTO(t))
		}
	}
	groups, rosterErr := w.rosterGroups(false)
	out.Roster.Warning = rosterErr
	out.Roster.Provenance = w.rosterProvenance(groups, rosterErr)
	for _, g := range groups {
		if g.Mission == slug {
			for _, a := range g.Agents {
				out.Roster.Agents = append(out.Roster.Agents, rosterAgentDTO(a))
			}
			break
		}
	}
	writeJSON(rw, http.StatusOK, out)
}
