package servecmd

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"ai-config/tools/herder/internal/fileroots"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomidentity"
)

// liveFleet is the request path's one answer to "which agents, and which
// roots": the roster the serve's own polls cached, and the root set built from
// it. Request handlers reach hcom only through it; the raw roster function
// (deps.roster, hcomidentity.List) stays with the pollers that feed the cache
// (cachingRoster, the life mirror, the state sweep). TestRequestPathReachesHcomOnlyThroughLiveFleet
// holds that line.
//
// An `hcom list` costs 0.2-0.5 s, more under load, and a root set build runs
// a `git rev-parse` per agent cwd; the service answers both from cache.
type liveFleet struct {
	roster     func() ([]hcomidentity.Row, error)
	cache      *rosterCache
	rootSets   *rootSetCache
	configured []string
	build      func(context.Context, []string, []hcomidentity.Row) (fileroots.Set, error)
}

func (deps dependencies) fleet() liveFleet {
	return liveFleet{
		roster:     deps.roster,
		cache:      deps.rosterCache,
		rootSets:   deps.rootSets,
		configured: deps.configuredRoots,
		build:      deps.roots,
	}
}

// Agents is the valid roster cached within RosterFreshness, if any; it never
// asks hcom.
func (f liveFleet) Agents() ([]hcomidentity.Row, bool) {
	cached, ok := f.cache.fresh(RosterFreshness)
	if !ok || fleetview.ValidateRoster(cached) != nil {
		return nil, false
	}
	return cached, true
}

// Changed tells the service the request just changed the roster (a launch
// joined an agent): the cached list stops being fresh, so the next read, a
// GET of the board included, asks hcom live rather than missing the agent for
// up to RosterFreshness.
func (f liveFleet) Changed() {
	f.cache.expire()
}

// Roster is the request path's agent list: the fresh cache, else a live ask
// whose answer is stored. A handler that must see a brand-new name uses
// RosterHolding instead.
func (f liveFleet) Roster() ([]hcomidentity.Row, error) {
	if cached, ok := f.Agents(); ok {
		return cached, nil
	}
	return f.Poll()
}

// RosterHolding is Roster for a handler about to look up name: a fresh cache
// that lacks name asks hcom live, so an agent that joined since the last poll
// is never refused.
func (f liveFleet) RosterHolding(name string) ([]hcomidentity.Row, error) {
	if cached, ok := f.Agents(); ok {
		for _, row := range cached {
			if row.Name == name {
				return cached, nil
			}
		}
	}
	return f.Poll()
}

// Poll asks hcom now and caches a valid answer stamped with when the ask
// began; a newer cached observation keeps the cache. It is for the SSE board
// poll (a cache refresher) and the miss paths above; the guard test allows
// it nowhere else on the request path.
func (f liveFleet) Poll() ([]hcomidentity.Row, error) {
	observed := f.cache.clock()
	roster, err := f.roster()
	if err != nil {
		return nil, err
	}
	if fleetview.ValidateRoster(roster) == nil {
		f.cache.setObserved(roster, observed)
	}
	return roster, nil
}

// Roots is the readable universe for a request: Roster mapped through the
// cached root set.
func (f liveFleet) Roots(ctx context.Context) (fileroots.Set, []hcomidentity.Row, error) {
	return f.rootsFrom(ctx, f.Roster)
}

// RootsAccepting returns the cached universe, or the live one when the cached
// one fails accept: a root or agent that appeared since the last poll is
// never refused on stale evidence.
func (f liveFleet) RootsAccepting(ctx context.Context, accept func(fileroots.Set, []hcomidentity.Row) bool) (fileroots.Set, []hcomidentity.Row, error) {
	set, rows, err := f.Roots(ctx)
	if err != nil || accept(set, rows) {
		return set, rows, err
	}
	return f.rootsFrom(ctx, f.Poll)
}

// RootsHolding is RootsAccepting for a request on root; directOK accepts an
// existing absolute directory outside the set (directOpenRoot) without the
// live retry.
func (f liveFleet) RootsHolding(ctx context.Context, root string, directOK bool) (fileroots.Set, error) {
	set, _, err := f.RootsAccepting(ctx, func(set fileroots.Set, _ []hcomidentity.Row) bool {
		return set.Contains(root) || directOK && directOpenRoot(root)
	})
	return set, err
}

func (f liveFleet) rootsFrom(ctx context.Context, read func() ([]hcomidentity.Row, error)) (fileroots.Set, []hcomidentity.Row, error) {
	rows, err := read()
	if err != nil {
		return fileroots.Set{}, nil, sourceError{"hcom", err}
	}
	if err := fleetview.ValidateRoster(rows); err != nil {
		return fileroots.Set{}, nil, sourceError{"hcom", fmt.Errorf("invalid roster: %w", err)}
	}
	set, err := f.rootSets.get(ctx, f.configured, rows, f.build)
	if err != nil {
		return fileroots.Set{}, nil, sourceError{"filesystem", err}
	}
	return set, rows, nil
}

// RootSetFreshness bounds how long a root set built from unchanged roster
// rows is reused. Building one runs a `git rev-parse` per agent cwd (dozens
// of them), so a request reuses the set while the rows' names and cwds are
// unchanged; the bound still catches a cwd whose repository came or went.
const RootSetFreshness = 30 * time.Second

// rootSetCache holds the one root set built from the roster rows it is keyed
// by. Its lock is held across a build, so concurrent requests that miss wait
// for that one build rather than each running git per cwd.
type rootSetCache struct {
	mu  sync.Mutex
	key string
	at  time.Time
	set fileroots.Set
}

func rootSetKey(configured []string, rows []hcomidentity.Row) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, row.Name+"\x00"+row.Directory)
	}
	slices.Sort(parts)
	return strings.Join(configured, "\x00") + "\x01" + strings.Join(parts, "\x01")
}

// get returns the set for rows; a nil cache builds per call.
func (c *rootSetCache) get(ctx context.Context, configured []string, rows []hcomidentity.Row, build func(context.Context, []string, []hcomidentity.Row) (fileroots.Set, error)) (fileroots.Set, error) {
	if c == nil {
		return build(ctx, configured, rows)
	}
	key := rootSetKey(configured, rows)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.key == key && now.Sub(c.at) < RootSetFreshness && !now.Before(c.at) {
		return c.set, nil
	}
	// The build outlives the request that started it: a cancelled request
	// must not cache a set missing the roots its git calls never answered.
	set, err := build(context.WithoutCancel(ctx), configured, rows)
	if err != nil {
		return fileroots.Set{}, err
	}
	c.key, c.at, c.set = key, now, set
	return set, nil
}
