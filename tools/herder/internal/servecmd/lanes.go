package servecmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-config/tools/herder/internal/lanecmd"
)

// laneMoveFile is the one-time move's {old space id: lane id} mapping, written
// by tools/fleet/lane-move.sh in the herder state dir and read by the web once
// per browser to re-key the owner's spaces.
const laneMoveFile = "lane-move.json"

type laneRequest struct {
	Name *string `json:"name"`
	Lead *string `json:"lead"`
}

type laneResponse struct {
	ID string `json:"id"`
}

// serveLaneNew is POST /api/lanes {name, lead}: `herder lane new`.
func serveLaneNew(w http.ResponseWriter, r *http.Request, deps dependencies) {
	var request laneRequest
	if err := decodeWriteBody(w, r, &request, false); err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if request.Name == nil || request.Lead == nil {
		refuse(w, http.StatusBadRequest, "bad request", "name and lead required")
		return
	}
	name, okName := laneValue(w, *request.Name)
	if !okName {
		return
	}
	lead, okLead := laneValue(w, *request.Lead)
	if !okLead {
		return
	}
	out, ok := execLane(w, r, deps, "new", name, "--lead", lead)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, laneResponse{ID: strings.TrimPrefix(strings.TrimSpace(out), "lane=")})
}

// serveLaneUpdate is POST /api/lanes/{id} {name} or {lead}: `herder lane
// rename` or `herder lane lead`.
func serveLaneUpdate(w http.ResponseWriter, r *http.Request, deps dependencies, id string) {
	var request laneRequest
	if err := decodeWriteBody(w, r, &request, false); err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	verb, value := "rename", request.Name
	if request.Lead != nil {
		verb, value = "lead", request.Lead
	}
	if (request.Name == nil) == (request.Lead == nil) {
		refuse(w, http.StatusBadRequest, "bad request", "one of name or lead required")
		return
	}
	arg, ok := laneValue(w, *value)
	if !ok {
		return
	}
	if _, ok := execLane(w, r, deps, verb, id, arg); ok {
		writeJSON(w, http.StatusOK, laneResponse{ID: id})
	}
}

// laneValue is a lane or seat name from a request body: trimmed, non-empty,
// and never read as a CLI flag.
func laneValue(w http.ResponseWriter, raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "-") {
		refuse(w, http.StatusBadRequest, "bad request", "a name must be non-empty and must not start with '-'")
		return "", false
	}
	return value, true
}

// execLane runs one lane verb as the attributed web viewer.
func execLane(w http.ResponseWriter, r *http.Request, deps dependencies, args ...string) (string, bool) {
	if deps.store == nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", "agent store unavailable")
		return "", false
	}
	roster, err := deps.fleet().Roster()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return "", false
	}
	sender, ok := webSender(w, r, deps, roster)
	if !ok {
		return "", false
	}
	var out bytes.Buffer
	err = lanecmd.Exec(deps.store, roster, deps.now(), append(args, "--by", sender, "--by-kind", "web"), &out)
	switch lanecmd.ExitCode(err) {
	case 0:
		deps.audit("lane time=%s viewer=%s args=%q", deps.now().UTC().Format(time.RFC3339Nano), sender, args)
		return out.String(), true
	case 1:
		refuse(w, http.StatusNotFound, "lane not found", err.Error())
	case 2:
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
	default:
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
	}
	return "", false
}

// serveLaneMoves is GET /api/lanes/moves: the one-time move's mapping, {}
// when the move has not run.
func serveLaneMoves(w http.ResponseWriter, deps dependencies) {
	moves := map[string]string{}
	if deps.store != nil {
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(deps.store.Dir), laneMoveFile))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
			return
		}
		if err == nil {
			if err := json.Unmarshal(raw, &moves); err != nil {
				refuse(w, http.StatusBadGateway, "lane move file unreadable", err.Error())
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, moves)
}
