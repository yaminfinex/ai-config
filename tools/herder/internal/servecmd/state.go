package servecmd

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"ai-config/tools/herder/internal/webstate"
)

type stateChange struct {
	Namespace string `json:"namespace"`
	Rev       uint64 `json:"rev"`
}

type stateChangeBroker struct {
	mu          sync.Mutex
	subscribers map[chan stateChange]struct{}
}

func newStateChangeBroker() *stateChangeBroker {
	return &stateChangeBroker{subscribers: map[chan stateChange]struct{}{}}
}

func (b *stateChangeBroker) subscribe() (<-chan stateChange, func()) {
	changes := make(chan stateChange, 16)
	b.mu.Lock()
	b.subscribers[changes] = struct{}{}
	b.mu.Unlock()
	return changes, func() {
		b.mu.Lock()
		delete(b.subscribers, changes)
		b.mu.Unlock()
	}
}

func (b *stateChangeBroker) publish(change stateChange) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for subscriber := range b.subscribers {
		select {
		case subscriber <- change:
		default:
			// State events are pull nudges, not the state itself. A saturated
			// subscriber will catch up from its cursor on its next event/reconnect.
		}
	}
}

type stateUpsertRequest struct {
	Rows []webstate.Row `json:"rows"`
}

type stateUpsertResponse struct {
	Accepted []string `json:"accepted"`
	Rev      uint64   `json:"rev"`
}

type stateSinceResponse struct {
	Rows []webstate.Row `json:"rows"`
	Rev  uint64         `json:"rev"`
}

func serveState(w http.ResponseWriter, r *http.Request, deps dependencies, namespace string) {
	if !webstate.ValidNamespace(namespace) {
		refuse(w, http.StatusNotFound, "unknown state namespace", "state namespace must be a short lowercase dotted identifier")
		return
	}
	roster, err := deps.roster()
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	user, err := attributedSender(r, deps, roster)
	if err != nil {
		serveAttributionError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		since := uint64(0)
		if raw := r.URL.Query().Get("since"); raw != "" {
			parsed, parseErr := strconv.ParseUint(raw, 10, 64)
			if parseErr != nil {
				refuse(w, http.StatusBadRequest, "bad request", "since must be a non-negative revision")
				return
			}
			since = parsed
		}
		rows, rev, stateErr := deps.state.Since(user, namespace, since)
		if stateErr != nil {
			serveStateError(w, stateErr)
			return
		}
		writeJSON(w, http.StatusOK, stateSinceResponse{Rows: rows, Rev: rev})
	case http.MethodPost:
		var request stateUpsertRequest
		if err := decodeWriteBody(w, r, &request, false); err != nil {
			if errors.Is(err, errWriteBodyTooLarge) {
				refuse(w, http.StatusRequestEntityTooLarge, "state batch too large", err.Error())
			} else {
				refuse(w, http.StatusBadRequest, "bad request", err.Error())
			}
			return
		}
		accepted, rev, stateErr := deps.state.Upsert(user, namespace, request.Rows)
		if stateErr != nil {
			serveStateError(w, stateErr)
			return
		}
		if len(accepted) > 0 {
			deps.stateChanges.publish(stateChange{Namespace: namespace, Rev: rev})
		}
		writeJSON(w, http.StatusOK, stateUpsertResponse{Accepted: accepted, Rev: rev})
	default:
		refuse(w, http.StatusBadRequest, "bad request", "GET or POST required")
	}
}

func serveStateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, webstate.ErrNamespaceNotFound):
		refuse(w, http.StatusNotFound, "state namespace not found", err.Error())
	case errors.Is(err, webstate.ErrValueTooLarge), errors.Is(err, webstate.ErrRowLimit):
		refuse(w, http.StatusRequestEntityTooLarge, "state refused", err.Error())
	case errors.Is(err, webstate.ErrUnavailable):
		refuse(w, http.StatusServiceUnavailable, "state unavailable", err.Error())
	default:
		short := "state refused"
		if strings.Contains(err.Error(), "invalid state namespace") {
			short = "unknown state namespace"
		}
		refuse(w, http.StatusBadRequest, short, err.Error())
	}
}
