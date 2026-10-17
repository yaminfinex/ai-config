// Package githubprs keeps the serve's read-only view of the GitHub pull
// requests its lanes care about: the PRs linked to a lane (`herder lane
// link`) and the open PRs whose description carries `<!-- lane: <id> -->`.
// Every interval it sends one GraphQL request per repository; the answers
// live in memory only and nothing writes to GitHub.
package githubprs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Interval is how often every wanted repository is asked again.
const Interval = 60 * time.Second

// Endpoint is GitHub's GraphQL API.
const Endpoint = "https://api.github.com/graphql"

// NoToken is what a lane says when the serve started without a token.
const NoToken = "no GitHub token: set GITHUB_TOKEN or run gh auth login, then restart the serve"

// Ref names one pull request; Repo is "owner/name", lower case.
type Ref struct {
	Repo   string
	Number int
}

func (r Ref) String() string { return r.Repo + "#" + strconv.Itoa(r.Number) }

var (
	shortRef = regexp.MustCompile(`^([A-Za-z0-9-]+/[A-Za-z0-9._-]+)#([0-9]+)$`)
	urlPath  = regexp.MustCompile(`^/([A-Za-z0-9-]+/[A-Za-z0-9._-]+)/pull/([0-9]+)(?:/.*)?$`)
	marker   = regexp.MustCompile(`<!--\s*lane:\s*([^\s>]+?)\s*-->`)
)

// ParseRef reads `owner/repo#123` or a github.com pull request URL.
func ParseRef(text string) (Ref, bool) {
	text = strings.TrimSpace(text)
	var repo, number string
	if match := shortRef.FindStringSubmatch(text); match != nil {
		repo, number = match[1], match[2]
	} else if parsed, err := url.Parse(text); err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && strings.EqualFold(strings.TrimPrefix(parsed.Hostname(), "www."), "github.com") {
		match := urlPath.FindStringSubmatch(parsed.Path)
		if match == nil {
			return Ref{}, false
		}
		repo, number = match[1], match[2]
	} else {
		return Ref{}, false
	}
	n, err := strconv.Atoi(number)
	if err != nil || n <= 0 {
		return Ref{}, false
	}
	return Ref{Repo: strings.ToLower(repo), Number: n}, true
}

// PR is one pull request as the board shows it. Lanes are the lane ids its
// description marks; Linked and Holder are set by the board, not GitHub.
type PR struct {
	Repo       string   `json:"repo"`
	Number     int      `json:"number"`
	Title      string   `json:"title"`
	URL        string   `json:"url"`
	State      string   `json:"state"` // OPEN | MERGED | CLOSED
	Draft      bool     `json:"draft"`
	Mergeable  string   `json:"mergeable,omitempty"` // MERGEABLE | CONFLICTING | UNKNOWN
	Checks     string   `json:"checks,omitempty"`    // statusCheckRollup.state; empty without checks
	Review     string   `json:"review,omitempty"`    // reviewDecision
	Unresolved int      `json:"unresolved"`          // review threads not resolved
	Head       string   `json:"head"`                // headRefName
	Linked     bool     `json:"linked,omitempty"`    // linked by `herder lane link`
	Holder     string   `json:"holder,omitempty"`    // a lane member on the head branch
	Lanes      []string `json:"-"`
}

// Post sends one GraphQL request and returns the response body.
type Post func(ctx context.Context, query string, variables map[string]any) ([]byte, error)

// Poller asks GitHub for the wanted repositories every interval.
type Poller struct {
	post  Post
	audit func(string, ...any)
	kick  chan struct{}

	mu sync.Mutex
	// want is each repository's linked numbers, set by the board.
	want map[string][]int
	// seen are numbers a marker named once: still fetched by number, so a
	// marked PR that merges shows merged rather than vanishing.
	seen  map[string][]int
	repos map[string]map[int]PR
}

// New makes a poller; a nil post means no token, and Problem says so.
func New(post Post, audit func(string, ...any)) *Poller {
	return &Poller{post: post, audit: audit, kick: make(chan struct{}, 1), want: map[string][]int{}, seen: map[string][]int{}, repos: map[string]map[int]PR{}}
}

// Token is GITHUB_TOKEN, else `gh auth token`, else empty.
func Token(ctx context.Context) string {
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return token
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// HTTPPost posts to endpoint with token.
func HTTPPost(client *http.Client, endpoint, token string) Post {
	return func(ctx context.Context, query string, variables map[string]any) ([]byte, error) {
		body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		var out bytes.Buffer
		if _, err := out.ReadFrom(response.Body); err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub answered %s: %.200s", response.Status, out.String())
		}
		return out.Bytes(), nil
	}
}

// Problem is the one line a lane shows instead of PRs, or empty.
func (p *Poller) Problem() string {
	if p.post == nil {
		return NoToken
	}
	return ""
}

// Want replaces the wanted repositories and their linked numbers. The first
// non-empty want starts the first poll; later changes wait for the interval.
func (p *Poller) Want(want map[string][]int) {
	p.mu.Lock()
	first := len(p.want) == 0 && len(want) > 0
	p.want = want
	p.mu.Unlock()
	if first {
		select {
		case p.kick <- struct{}{}:
		default:
		}
	}
}

// Get is one PR by ref, if a poll has seen it.
func (p *Poller) Get(ref Ref) (PR, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pr, ok := p.repos[ref.Repo][ref.Number]
	return pr, ok
}

// Marked is every PR whose description marks lane, by repo then number.
func (p *Poller) Marked(lane string) []PR {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []PR
	for _, prs := range p.repos {
		for _, pr := range prs {
			if slices.Contains(pr.Lanes, lane) {
				out = append(out, pr)
			}
		}
	}
	slices.SortFunc(out, func(a, b PR) int {
		if c := strings.Compare(a.Repo, b.Repo); c != 0 {
			return c
		}
		return a.Number - b.Number
	})
	return out
}

// Run polls every interval until ctx ends. Without a token it does nothing.
func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	if p.post == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.kick:
		case <-ticker.C:
		}
		p.Poll(ctx)
	}
}

// Poll sends one request per wanted repository and keeps the answers. A
// repository that is no longer wanted is forgotten; a failed request keeps
// that repository's last answer.
func (p *Poller) Poll(ctx context.Context) {
	p.mu.Lock()
	want := make(map[string][]int, len(p.want))
	for repo, numbers := range p.want {
		want[repo] = union(numbers, p.seen[repo])
	}
	for repo := range p.repos {
		if _, ok := want[repo]; !ok {
			delete(p.repos, repo)
			delete(p.seen, repo)
		}
	}
	p.mu.Unlock()
	for repo, numbers := range want {
		started := time.Now()
		prs, err := p.fetch(ctx, repo, numbers)
		if err != nil {
			p.audit("herder serve: github prs: %s: %v", repo, err)
			continue
		}
		p.audit("herder serve: github prs: %s: %d linked, %d PRs (%s)", repo, len(numbers), len(prs), time.Since(started).Round(time.Millisecond))
		p.mu.Lock()
		p.repos[repo] = prs
		for number, pr := range prs {
			if len(pr.Lanes) > 0 && !slices.Contains(p.seen[repo], number) {
				p.seen[repo] = append(p.seen[repo], number)
			}
		}
		p.mu.Unlock()
	}
}

func union(a, b []int) []int {
	out := slices.Clone(a)
	for _, n := range b {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

const fragment = `fragment pr on PullRequest {
  number title url state isDraft mergeable headRefName reviewDecision body
  statusCheckRollup { state }
  reviewThreads(first: 100) { nodes { isResolved } }
}`

// Query is the one request for a repository: its open PRs (for markers) and
// each linked number by alias.
func Query(numbers []int) string {
	var aliases strings.Builder
	for _, n := range numbers {
		fmt.Fprintf(&aliases, "    n%d: pullRequest(number: %d) { ...pr }\n", n, n)
	}
	return "query($owner: String!, $name: String!) {\n  repository(owner: $owner, name: $name) {\n" +
		"    open: pullRequests(states: OPEN, first: 100, orderBy: {field: UPDATED_AT, direction: DESC}) { nodes { ...pr } }\n" +
		aliases.String() + "  }\n}\n" + fragment
}

type node struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	State          string `json:"state"`
	IsDraft        bool   `json:"isDraft"`
	Mergeable      string `json:"mergeable"`
	HeadRefName    string `json:"headRefName"`
	ReviewDecision string `json:"reviewDecision"`
	Body           string `json:"body"`
	Rollup         *struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
	ReviewThreads struct {
		Nodes []struct {
			IsResolved bool `json:"isResolved"`
		} `json:"nodes"`
	} `json:"reviewThreads"`
}

func (p *Poller) fetch(ctx context.Context, repo string, numbers []int) (map[int]PR, error) {
	owner, name, _ := strings.Cut(repo, "/")
	body, err := p.post(ctx, Query(numbers), map[string]any{"owner": owner, "name": name})
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("read GitHub answer: %w", err)
	}
	// A linked number that is not a PR answers NOT_FOUND beside the rest;
	// only a missing repository fails the whole request.
	for _, problem := range response.Errors {
		p.audit("herder serve: github prs: %s: %s", repo, problem.Message)
	}
	if response.Data.Repository == nil {
		return nil, fmt.Errorf("no repository in the answer")
	}
	prs := map[int]PR{}
	add := func(n node) {
		pr := PR{Repo: repo, Number: n.Number, Title: n.Title, URL: n.URL, State: n.State, Draft: n.IsDraft,
			Mergeable: n.Mergeable, Review: n.ReviewDecision, Head: n.HeadRefName}
		if n.Rollup != nil {
			pr.Checks = n.Rollup.State
		}
		for _, thread := range n.ReviewThreads.Nodes {
			if !thread.IsResolved {
				pr.Unresolved++
			}
		}
		for _, match := range marker.FindAllStringSubmatch(n.Body, -1) {
			if !slices.Contains(pr.Lanes, match[1]) {
				pr.Lanes = append(pr.Lanes, match[1])
			}
		}
		prs[n.Number] = pr
	}
	var open struct {
		Nodes []node `json:"nodes"`
	}
	if raw := response.Data.Repository["open"]; raw != nil {
		if err := json.Unmarshal(raw, &open); err != nil {
			return nil, fmt.Errorf("read open PRs: %w", err)
		}
	}
	for _, n := range open.Nodes {
		add(n)
	}
	for _, number := range numbers {
		var n *node
		if raw := response.Data.Repository["n"+strconv.Itoa(number)]; raw != nil {
			if err := json.Unmarshal(raw, &n); err != nil {
				return nil, fmt.Errorf("read PR %d: %w", number, err)
			}
		}
		if n != nil {
			add(*n)
		}
	}
	return prs, nil
}
