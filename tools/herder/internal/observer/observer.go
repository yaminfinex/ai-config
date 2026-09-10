// Package observer keeps live session vitals in memory for a running
// `herder serve`. One goroutine owns one record per (tool, session, agent_id)
// discovered from the hcom roster, seeds it with the same reader the direct
// CLI path uses (sessionvitals.Seed) and keeps it current by tailing the
// transcript directory with fsnotify (sessionvitals.Advance).
//
// It deliberately does NOT persist anything (vitals are never durable — a
// restart re-seeds from transcripts), does NOT parse transcripts itself, does
// NOT serve HTTP or the socket (servecmd hands Lookup to herdersock), and
// does NOT gate any lifecycle action. Lookup is the only read API.
//
// File map: observer.go (options, constants, run loop, Lookup), session.go
// (key, phases, per-session seed/advance), discover.go (roster → sessions),
// watch.go (directory watcher with a ceiling and the sweep it degrades to).
package observer

import (
	"context"
	"os"
	"sync"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/sessionvitals"
)

// Cadences and limits. One place, one reason each.
const (
	// DefaultPoll is the roster discovery cadence; it matches the serve's
	// PollCadence so the observer learns a new or resumed session as fast as
	// the board does.
	DefaultPoll = 2 * time.Second
	// DefaultSweep is the safety cadence: every non-ended session is stat'ed,
	// which catches missed inotify events and serves sessions above the
	// directory ceiling. Matches servecmd.TranscriptSafetyCadence.
	DefaultSweep = 30 * time.Second
	// DefaultTTL keeps an ended session's last vitals answerable for a day
	// (a retired agent someone still has open), then drops it.
	DefaultTTL = 24 * time.Hour
	// WatchDebounce coalesces a burst of writes into one Advance, the same
	// window the per-connection transcript push uses.
	WatchDebounce = 120 * time.Millisecond
	// MaxWatchDirectories caps inotify use; sessions beyond it are not an
	// error, they live on the sweep. Same ceiling as servecmd's file watches.
	MaxWatchDirectories = 64
)

// Options are the seams: Roster is the discovery source, Now the clock (TTL
// tests), Watcher the fsnotify factory (nil = sweep only). Zero cadences take
// the defaults.
type Options struct {
	Roster  func() ([]hcomidentity.Row, error)
	Now     func() time.Time
	Watcher WatcherFactory
	Home    string
	Poll    time.Duration
	Sweep   time.Duration
	TTL     time.Duration
	Audit   func(string, ...any)
}

// Observer is the in-memory session table. Construct with New, start with
// Run, read with Lookup.
type Observer struct {
	opts     Options
	mu       sync.RWMutex
	sessions map[Key]*session
	byName   map[string]Key
	watch    *dirWatch
}

// New fills defaults; it does no I/O.
func New(opts Options) *Observer {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Poll <= 0 {
		opts.Poll = DefaultPoll
	}
	if opts.Sweep <= 0 {
		opts.Sweep = DefaultSweep
	}
	if opts.TTL <= 0 {
		opts.TTL = DefaultTTL
	}
	if opts.Audit == nil {
		opts.Audit = func(string, ...any) {}
	}
	if opts.Home == "" {
		opts.Home, _ = os.UserHomeDir()
	}
	return &Observer{opts: opts, sessions: map[Key]*session{}, byName: map[string]Key{}}
}

// Run seeds every live roster session once, synchronously, so the first
// lookup is warm, then owns the table until ctx ends: roster poll, debounced
// directory changes, safety sweep. Only this goroutine mutates the table.
func (o *Observer) Run(ctx context.Context) {
	o.watch = startDirWatch(ctx, o.opts.Watcher, o.opts.Audit)
	o.pollRoster()
	go o.loop(ctx)
}

func (o *Observer) loop(ctx context.Context) {
	poll := time.NewTicker(o.opts.Poll)
	defer poll.Stop()
	sweep := time.NewTicker(o.opts.Sweep)
	defer sweep.Stop()
	var changes <-chan []string
	if o.watch != nil {
		changes = o.watch.Changes
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			o.pollRoster()
		case paths := <-changes:
			o.onPaths(paths)
		case <-sweep.C:
			o.sweep()
		}
	}
}

// Lookup is the cache seam sessionvitals.ReadWith takes. The miss rule lives
// here and nowhere else: unknown session, or a known one that has no vitals
// yet (awaiting_file, a seed that failed) is a miss; an ended session within
// the TTL is still a hit with its last vitals.
func (o *Observer) Lookup(row hcomidentity.Row) (sessionvitals.Result, bool) {
	snapshot, ok := o.Snapshot(KeyFor(row))
	if !ok || snapshot.Vitals.Model == "" && snapshot.Vitals.ContextUsage == nil {
		return sessionvitals.Result{}, false
	}
	return sessionvitals.Result{Vitals: snapshot.Vitals, Path: snapshot.Path, ObservedAt: snapshot.ObservedAt}, true
}

// Snapshot returns a copy of one session's state (tests, diagnostics).
func (o *Observer) Snapshot(key Key) (Snapshot, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	s, ok := o.sessions[key]
	if !ok {
		return Snapshot{}, false
	}
	return s.snapshot(), true
}

// Len is the number of tracked sessions, ended ones included (tests).
func (o *Observer) Len() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return len(o.sessions)
}

func (o *Observer) pollRoster() {
	if o.opts.Roster == nil {
		return
	}
	rows, err := o.opts.Roster()
	if err != nil {
		o.opts.Audit("observer: roster read failed; keeping last sessions: %v", err)
		return
	}
	o.sync(rows)
}
