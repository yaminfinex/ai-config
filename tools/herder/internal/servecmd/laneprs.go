package servecmd

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/gitapi"
	"ai-config/tools/herder/internal/githubprs"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/repoctx"
)

// foldBoardLanes fills each board lane's repos, branches and PRs. A lane's
// repos are its members' GitHub origins plus `herder lane repo`; its
// branches are its members' branches. Its PRs are the linked ones, in link
// order (a link not polled yet is a bare row), then the open ones whose
// description marks the lane. It tells the poller what to ask next: every
// lane repo and every linked number, so a board with no lanes stops the
// polling. A nil poller leaves PRs empty.
func foldBoardLanes(board *fleetview.Board, roster []hcomidentity.Row, proj *agentstore.Projection, git func(cwd string) *repoctx.Git, prs *githubprs.Poller) {
	directory := make(map[string]string, len(roster))
	for _, row := range roster {
		directory[row.Name] = row.Directory
	}
	want := map[string][]int{}
	for index := range board.Lanes {
		lane := &board.Lanes[index]
		var record *agentstore.LaneView
		if proj != nil {
			record = proj.Lanes[lane.ID]
		}
		type checkout struct{ repo, branch string }
		held := map[string]checkout{}
		for _, member := range lane.Members {
			cwd := directory[member]
			if cwd == "" {
				continue
			}
			facts := git(cwd)
			if facts == nil {
				continue
			}
			var repo string
			if owner, name, ok := gitapi.ParseGitHubRemote(facts.RemoteURL); ok {
				repo = strings.ToLower(owner + "/" + name)
				lane.Repos = appendNew(lane.Repos, repo)
			}
			if facts.Branch != "" {
				lane.Branches = appendNew(lane.Branches, facts.Branch)
				held[member] = checkout{repo, facts.Branch}
			}
		}
		if record != nil {
			for _, repo := range record.Repos {
				lane.Repos = appendNew(lane.Repos, strings.ToLower(repo))
			}
		}
		slices.Sort(lane.Repos)
		slices.Sort(lane.Branches)
		if prs == nil {
			continue
		}
		for _, repo := range lane.Repos {
			if _, ok := want[repo]; !ok {
				want[repo] = nil
			}
		}
		if record != nil {
			for _, text := range record.PRs {
				ref, ok := githubprs.ParseRef(text)
				if !ok {
					continue
				}
				want[ref.Repo] = appendNew(want[ref.Repo], ref.Number)
				pr, polled := prs.Get(ref)
				if !polled {
					pr = githubprs.PR{Repo: ref.Repo, Number: ref.Number, URL: "https://github.com/" + ref.Repo + "/pull/" + strconv.Itoa(ref.Number)}
				}
				pr.Linked = true
				lane.PRs = append(lane.PRs, pr)
			}
		}
		for _, pr := range prs.Marked(lane.ID) {
			if !slices.ContainsFunc(lane.PRs, func(have githubprs.PR) bool { return have.Repo == pr.Repo && have.Number == pr.Number }) {
				lane.PRs = append(lane.PRs, pr)
			}
		}
		for i := range lane.PRs {
			for _, member := range lane.Members {
				if at, ok := held[member]; ok && at.repo == lane.PRs[i].Repo && at.branch == lane.PRs[i].Head {
					lane.PRs[i].Holder = member
					break
				}
			}
		}
		if len(lane.Repos) > 0 || len(lane.PRs) > 0 {
			lane.PRNote = prs.Problem()
		}
	}
	if prs != nil {
		prs.Want(want)
	}
}

func appendNew[T comparable](values []T, value T) []T {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

// repoContextCache keeps each cwd's git facts for RootSetFreshness, so a
// board poll does not run three git processes per lane member.
type repoContextCache struct {
	mu      sync.Mutex
	entries map[string]cachedRepoContext
}

type cachedRepoContext struct {
	at  time.Time
	git *repoctx.Git
}

// reader is the lookup for one board build; a nil cache reads every time.
func (c *repoContextCache) reader(ctx context.Context, read func(context.Context, string) (repoctx.Context, error)) func(string) *repoctx.Git {
	return func(cwd string) *repoctx.Git {
		if c != nil {
			c.mu.Lock()
			entry, ok := c.entries[cwd]
			c.mu.Unlock()
			if ok && time.Since(entry.at) < RootSetFreshness {
				return entry.git
			}
		}
		// The read outlives a cancelled request, which must not cache a cwd
		// its git calls never answered.
		facts, err := read(context.WithoutCancel(ctx), cwd)
		if err != nil {
			return nil
		}
		if c != nil {
			c.mu.Lock()
			if c.entries == nil {
				c.entries = map[string]cachedRepoContext{}
			}
			c.entries[cwd] = cachedRepoContext{time.Now(), facts.Git}
			c.mu.Unlock()
		}
		return facts.Git
	}
}

// startPRPoller reads the GitHub token once and polls every
// githubprs.Interval. Without a token the lanes say so and nothing is asked.
func startPRPoller(ctx context.Context, audit func(string, ...any)) *githubprs.Poller {
	var post githubprs.Post
	if token := githubprs.Token(ctx); token != "" {
		post = githubprs.HTTPPost(&http.Client{Timeout: 20 * time.Second}, githubprs.Endpoint, token)
	} else {
		audit("herder serve: %s", githubprs.NoToken)
	}
	poller := githubprs.New(post, audit)
	go poller.Run(ctx, githubprs.Interval)
	return poller
}
