package hcomevents

import "sync"

// TurnEnds folds listening entries into each agent's latest completed turn:
// the id of the newest status event where that agent went from active or
// blocked to listening. The id is hcom's monotonic event id, so a browser
// can hold it as a read marker. Launch (pending) and reattach (inactive)
// entries are not completed turns; they only carry the session forward. A
// new session is a new incarnation of the name and clears the turn it had.
// Keys are hcom instance (base) names.
type TurnEnds struct {
	mu     sync.RWMutex
	agents map[string]turnEnd
}

type turnEnd struct {
	session string
	id      int64
}

func NewTurnEnds() *TurnEnds {
	return &TurnEnds{agents: map[string]turnEnd{}}
}

func (t *TurnEnds) Apply(status Status) {
	if status.Instance == "" || status.NewStatus != "listening" || status.OldStatus == "listening" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.agents[status.Instance]
	if status.Session != "" && status.Session != current.session {
		current = turnEnd{session: status.Session}
	}
	if (status.OldStatus == "active" || status.OldStatus == "blocked") && status.ID > current.id {
		current.id = status.ID
	}
	t.agents[status.Instance] = current
}

// Lookup returns the agent's latest turn-end id for exactly the given
// session, or false while that incarnation has not finished a turn this
// serve has seen: an older session's turn never answers for a newer one.
func (t *TurnEnds) Lookup(instance, session string) (int64, bool) {
	if t == nil || session == "" {
		return 0, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	current, ok := t.agents[instance]
	if !ok || current.session != session || current.id <= 0 {
		return 0, false
	}
	return current.id, true
}
