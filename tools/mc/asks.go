package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// askEntity is mc's normalized view of mish.ask/v1. mc deliberately knows
// nothing about the entity files: mish status/view are the only read boundary.
type askEntity struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	State       string         `json:"state"`
	Outcome     string         `json:"outcome"`
	Asker       string         `json:"asker"`
	AddressedTo string         `json:"addressed_to"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	Expects     string         `json:"expects"`
	Blocking    *askBlocking   `json:"blocking"`
	Anchor      askReference   `json:"anchor"`
	Links       []askReference `json:"links"`
	Members     []string       `json:"members"`
	Framing     askFraming     `json:"framing"`
	Replies     []askReply     `json:"replies"`
	Rulings     []askRuling    `json:"ruling_trail"`
	Traces      []askTrace     `json:"traces"`
	Warnings    []string       `json:"warnings"`
	Mission     string         `json:"-"`
}

type askBlocking struct{ Fact, Actor, At string }

type askReference struct {
	Type string `json:"type"`
	Ref  string `json:"ref"`
}

type askFraming struct {
	Context        string             `json:"context"`
	Question       string             `json:"question"`
	SubDecisions   []string           `json:"sub_decisions"`
	Options        []askOption        `json:"options"`
	Recommendation *askRecommendation `json:"recommendation"`
	DoNothing      string             `json:"do_nothing"`
}

type askOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Cost        string `json:"cost"`
	Risk        string `json:"risk"`
	BlastRadius string `json:"blast_radius"`
}

type askRecommendation struct {
	Choice string `json:"choice"`
	Reason string `json:"reason"`
}

type askReply struct{ ID, Actor, At, Prose string }

type askRuling struct {
	Question           string      `json:"question"`
	OptionsAsPresented []askOption `json:"options_as_presented"`
	Choice             string      `json:"choice"`
	Prose              string      `json:"prose"`
	Actor              string      `json:"actor"`
	At                 string      `json:"at"`
}

type askTrace struct {
	Action   string        `json:"action"`
	Actor    string        `json:"actor"`
	At       string        `json:"at"`
	Outcome  string        `json:"outcome"`
	Reason   string        `json:"reason"`
	Citation *askReference `json:"citation"`
	Member   string        `json:"member"`
}

type missionAsks struct {
	Available bool              `json:"available"`
	Counts    []missionAskCount `json:"counts"`
	Total     int               `json:"total"`
	Entities  []askEntity       `json:"entities"`
}

type missionAskCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

type askEnvelope struct {
	OK         bool      `json:"ok"`
	Verb       string    `json:"verb"`
	Slug       string    `json:"slug"`
	MissionDir string    `json:"mission_dir"`
	Entity     askEntity `json:"entity"`
	Warnings   []string  `json:"warnings"`
	Refusal    string    `json:"refusal"`
	Reason     string    `json:"reason"`
	Remedy     string    `json:"remedy"`
}

func (m *missionResolver) Ask(slug, id string) (askEntity, askEnvelope) {
	var envelope askEnvelope
	if m == nil || m.bin == "" {
		envelope.Reason = "ask is unavailable: mish is disabled"
		return askEntity{}, envelope
	}
	out, _ := m.run("asks", "--mission", slug, "view", id)
	if err := json.Unmarshal(bytes.TrimSpace(out), &envelope); err != nil {
		envelope.Reason = "mish asks view returned unreadable JSON"
		return askEntity{}, envelope
	}
	envelope.Entity.Mission = slug
	envelope.Entity.Warnings = append(envelope.Entity.Warnings, envelope.Warnings...)
	return envelope.Entity, envelope
}

func (m *missionResolver) FindAsk(id string) (askEntity, missionStatus, bool) {
	statuses, _, _ := m.AllStatuses()
	for _, status := range statuses {
		for _, entity := range status.Asks.Entities {
			if entity.ID == id {
				entity.Mission = status.Slug
				return entity, status, true
			}
		}
	}
	return askEntity{}, missionStatus{}, false
}

func (m *missionResolver) MutateAsk(slug, verb, id string, input any) askEnvelope {
	var envelope askEnvelope
	if m == nil || m.bin == "" {
		envelope.Reason = "ask write is unavailable: mish is disabled"
		return envelope
	}
	payload, err := json.Marshal(input)
	if err != nil {
		envelope.Reason = err.Error()
		return envelope
	}
	args := []string{"asks", "--mission", slug, verb, id, "--input", "-"}
	cmd := exec.Command(m.bin, args...)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = m.commandEnv()
	out, runErr := cmd.Output()
	if err := json.Unmarshal(bytes.TrimSpace(out), &envelope); err != nil {
		envelope.Reason = "mish asks " + verb + " returned unreadable JSON"
		if runErr != nil {
			envelope.Reason = fmt.Sprintf("mish asks %s failed: %v", verb, runErr)
		}
		return envelope
	}
	if envelope.OK {
		envelope.Entity.Mission = slug
		m.invalidateStatus(slug)
	}
	return envelope
}

func (m *missionResolver) commandEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "MISSIONS_REPO=") {
			env = append(env, kv)
		}
	}
	if m.repo != "" {
		env = append(env, "MISSIONS_REPO="+m.repo)
	}
	return env
}

func (m *missionResolver) invalidateStatus(slug string) {
	m.mu.Lock()
	delete(m.statusHits, slug)
	m.allHit = missionListHit{}
	m.mu.Unlock()
}
