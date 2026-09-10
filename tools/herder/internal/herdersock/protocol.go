// Package herdersock is the local socket between the herder CLI and a running
// `herder serve`. It owns the socket path, the line framing, the request and
// response types, the client's time budget and the server's lifecycle — both
// ends in one package so they cannot drift.
//
// It deliberately does NOT know about transcripts or the observer: the server
// takes one answer function and the client returns bytes decoded into
// Response. Today there is one op, "vitals". Future directions, signposted and
// not built here: more ops (board, list); a `vitals` SSE event on the HTTP
// side; the CLI becoming a pure client of the serve; the observer moving to a
// separate daemon process (the path and framing would not change); keep-alive
// connections carrying several requests (today: one request per connection).
package herdersock

import (
	"path/filepath"
	"time"

	"ai-config/tools/herder/internal/claudesession"
)

// Cadences and limits. One place per package, one reason each.
const (
	// SocketName lives under the herder state dir, so a scratch
	// HERDER_STATE_DIR (tests, lane serves) never reaches the real socket.
	SocketName = "herder.sock"
	// ClientBudget bounds ONLY a listener that accepts and then stalls (a hung
	// serve). Absent or stale sockets are detected without waiting.
	ClientBudget = 150 * time.Millisecond
	// ServerDeadline bounds one client's request read and reply write so a
	// stuck client cannot pin a server goroutine.
	ServerDeadline = time.Second
	// OpVitals is the single request kind: the observer's vitals for one
	// session key.
	OpVitals = "vitals"
	// SourceCache and SourceDirect are the two answers to "where did these
	// vitals come from"; the same words appear in Go, JSON and `show --json`.
	SourceCache  = "cache"
	SourceDirect = "direct"
)

// Path is the socket path for a state dir.
func Path(stateDir string) string { return filepath.Join(stateDir, SocketName) }

// Request is one newline-terminated JSON object. Tool, Session and AgentID
// form the observer's session key (AgentID only for Claude subagents).
type Request struct {
	Op      string `json:"op"`
	Tool    string `json:"tool"`
	Session string `json:"session"`
	AgentID string `json:"agent_id,omitempty"`
}

// Response is one newline-terminated JSON object. Miss means the serve does
// not know the session (the caller reads the transcript directly). Vitals is
// claudesession.Vitals' own encoding so numbers are byte-identical with a
// direct read.
type Response struct {
	Miss       bool                 `json:"miss,omitempty"`
	Error      string               `json:"error,omitempty"`
	Vitals     claudesession.Vitals `json:"vitals"`
	Path       string               `json:"path,omitempty"`
	ObservedAt time.Time            `json:"observed_at"`
}

// IsHit is the ONE definition of a usable answer: not a miss, not an error,
// and carrying at least a model or a context usage. `{}` and `null` decode
// without error and are misses, so the caller falls back to the direct read.
func (r Response) IsHit() bool {
	return !r.Miss && r.Error == "" && (r.Vitals.Model != "" || r.Vitals.ContextUsage != nil)
}
