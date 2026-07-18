package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// /api/v1 is the JSON face of the same structs the HTML pages render.
// DTOs are explicit wire contracts with tools/mc/ui: internal store types
// never cross wholesale, so store refactors cannot silently change the API.
// Degraded upstream reads (mish/hcom/herder unavailable) stay HTTP 200 with
// their warnings in the payload — the UI renders honesty, not a blank error.

type apiVersionDTO struct {
	Cursor     int64  `json:"cursor"`
	Generation uint64 `json:"generation"`
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
	Missions []apiMissionDTO `json:"missions"`
	Warning  string          `json:"warning"`
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
	Name      string `json:"name"`
	Address   string `json:"address"`
	Tool      string `json:"tool"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	Unread    int    `json:"unread"`
	Role      string `json:"role"`
	Branch    string `json:"branch"`
	Unmanaged bool   `json:"unmanaged"`
}

type apiMissionDetailDTO struct {
	Mission       apiMissionDTO       `json:"mission"`
	Threads       []apiThreadDTO      `json:"threads"`
	Agents        []apiRosterAgentDTO `json:"agents"`
	RosterWarning string              `json:"rosterWarning"`
}

func writeJSON(rw http.ResponseWriter, code int, v any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	rw.WriteHeader(code)
	if err := json.NewEncoder(rw).Encode(v); err != nil {
		log.Printf("api encode: %v", err)
	}
}

func (w *Web) apiVersion(rw http.ResponseWriter, _ *http.Request) {
	cursor, generation := w.store.Version()
	writeJSON(rw, http.StatusOK, apiVersionDTO{Cursor: cursor, Generation: generation})
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
		Name:      a.Name,
		Address:   a.Address,
		Tool:      a.Tool,
		Status:    a.Status,
		Detail:    a.Detail,
		Unread:    a.Unread,
		Role:      a.Role,
		Branch:    a.Branch,
		Unmanaged: a.Unmanaged,
	}
}

func (w *Web) apiMissions(rw http.ResponseWriter, _ *http.Request) {
	statuses, warning := w.missions.AllStatuses()
	out := apiMissionsDTO{Missions: []apiMissionDTO{}, Warning: warning}
	for _, s := range statuses {
		out.Missions = append(out.Missions, missionDTO(s))
	}
	writeJSON(rw, http.StatusOK, out)
}

func (w *Web) apiMission(rw http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	status := w.missions.Status(slug)
	if status.Slug == "" {
		status.Slug = slug
	}
	out := apiMissionDetailDTO{
		Mission: missionDTO(status),
		Threads: []apiThreadDTO{},
		Agents:  []apiRosterAgentDTO{},
	}
	for _, t := range w.store.List("", "") {
		if t.Home == slug {
			out.Threads = append(out.Threads, threadDTO(t))
		}
	}
	groups, rosterErr := w.rosterGroups(false)
	out.RosterWarning = rosterErr
	for _, g := range groups {
		if g.Mission == slug {
			for _, a := range g.Agents {
				out.Agents = append(out.Agents, rosterAgentDTO(a))
			}
			break
		}
	}
	writeJSON(rw, http.StatusOK, out)
}
