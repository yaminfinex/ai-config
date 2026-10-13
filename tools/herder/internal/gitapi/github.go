package gitapi

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// githubHosts are the hosts GitHub itself documents for repository remotes.
// ssh.github.com is its SSH-over-443 endpoint. Any other host, including an
// SSH config alias, is not proved to be GitHub: Git's own insteadOf rewriting
// is already applied by `git remote get-url`.
var githubHosts = map[string]bool{"github.com": true, "www.github.com": true, "ssh.github.com": true}

var (
	githubOwner = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)
	githubRepo  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	scpRemote   = regexp.MustCompile(`^(?:[^@/:]+@)?([^/:]+):(.*)$`)
)

// ParseGitHubRemote returns owner and repo for a remote URL that names a
// GitHub repository, and ok=false for every other remote.
func ParseGitHubRemote(remote string) (owner, repo string, ok bool) {
	remote = strings.TrimSpace(remote)
	var host, path string
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err != nil {
			return "", "", false
		}
		switch parsed.Scheme {
		case "https", "http", "ssh", "git", "git+ssh", "ssh+git":
		default:
			return "", "", false
		}
		host, path = parsed.Hostname(), parsed.Path
	} else if match := scpRemote.FindStringSubmatch(remote); match != nil {
		host, path = match[1], match[2]
	} else {
		return "", "", false
	}
	if !githubHosts[strings.ToLower(host)] {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return "", "", false
	}
	owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
	if !githubOwner.MatchString(owner) || !githubRepo.MatchString(repo) || repo == "." || repo == ".." {
		return "", "", false
	}
	return owner, repo, true
}

// onOriginCache remembers checkouts whose HEAD commit is proved reachable from
// an origin remote-tracking ref. Only proofs are cached: a push can turn "not
// on origin" into "on origin" at any time, but a pushed commit stays pushed.
var onOriginCache = struct {
	sync.Mutex
	proved map[string]bool
}{proved: map[string]bool{}}

const onOriginCacheLimit = 1024

func cachedOnOrigin(key string) bool {
	onOriginCache.Lock()
	defer onOriginCache.Unlock()
	return onOriginCache.proved[key]
}

func rememberOnOrigin(key string) {
	onOriginCache.Lock()
	defer onOriginCache.Unlock()
	if len(onOriginCache.proved) >= onOriginCacheLimit {
		onOriginCache.proved = map[string]bool{}
	}
	onOriginCache.proved[key] = true
}

// ReadGitHub locates root-relative requestedPath (empty means the root itself)
// on GitHub. A path that is not in a checkout with a GitHub origin, or that
// does not exist at the commit the link would name, is a successful result
// with GitHub omitted and Reason saying why.
func ReadGitHub(ctx context.Context, root, requestedPath string, now func() time.Time) (GitHubResult, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	result := GitHubResult{Root: root, Path: requestedPath, FetchedAt: now()}
	var loc location
	var err error
	if requestedPath == "" {
		loc, err = discover(ctx, root)
		if err == nil {
			loc.repoPath = filepath.ToSlash(loc.rootPrefix)
		}
	} else {
		loc, err = locatePath(ctx, root, requestedPath)
		if err != nil && !errors.Is(err, ErrUnavailable) {
			return GitHubResult{}, err
		}
	}
	if err != nil {
		result.Reason = "not a git repository"
		return result, nil
	}
	result.Root = loc.root
	if loc.repoPath == "." {
		loc.repoPath = ""
	}
	remote, err := gitOutput(ctx, loc.repoTop, "remote", "get-url", "origin")
	if err != nil {
		result.Reason = "this checkout has no origin remote"
		return result, nil
	}
	owner, repo, ok := ParseGitHubRemote(string(remote))
	if !ok {
		result.Reason = "origin is not a GitHub repository"
		return result, nil
	}
	ref, reason := githubRef(ctx, loc.repoTop)
	if ref.sha == "" {
		result.Reason = reason
		return result, nil
	}
	kind := "tree"
	if loc.repoPath != "" {
		typeOut, typeErr := gitOutput(ctx, loc.repoTop, "cat-file", "-t", ref.sha+":"+loc.repoPath)
		kind = strings.TrimSpace(string(typeOut))
		if typeErr != nil || kind != "blob" && kind != "tree" {
			result.Reason = "this path is not on " + ref.label
			return result, nil
		}
	}
	pathspec := ":(top,literal)" + loc.repoPath
	if loc.repoPath == "" {
		pathspec = ":(top)"
	}
	uncommitted, err := pathUncommitted(ctx, loc.repoTop, pathspec)
	if err != nil {
		result.Reason = compactReason(err)
		return result, nil
	}
	differs := uncommitted
	if !ref.atHead {
		code, diffErr := gitExitCode(ctx, loc.repoTop, "diff", "--quiet", "--no-ext-diff", "--no-textconv", ref.sha, "--", pathspec)
		if diffErr != nil {
			result.Reason = compactReason(diffErr)
			return result, nil
		}
		differs = differs || code == 1
	}
	result.GitHub = &GitHubLocation{
		Owner: owner, Repo: repo, Path: loc.repoPath, Kind: kind, SHA: ref.sha,
		Ref: ref.label, AtHead: ref.atHead, Uncommitted: uncommitted, Differs: differs,
	}
	return result, nil
}

type linkRef struct {
	sha    string
	label  string
	atHead bool
}

// githubRef picks the commit a GitHub link names: HEAD when an origin
// remote-tracking ref contains it, otherwise the origin upstream of the
// current branch, otherwise origin/HEAD. The cheap ancestor tests against the
// upstream and origin/HEAD run before the full scan of origin's refs.
func githubRef(ctx context.Context, top string) (linkRef, string) {
	upstream := ""
	if out, err := gitOutput(ctx, top, "rev-parse", "--symbolic-full-name", "@{upstream}"); err == nil {
		if name := strings.TrimSpace(string(out)); strings.HasPrefix(name, "refs/remotes/origin/") {
			upstream = name
		}
	}
	originHead := ""
	if out, err := gitOutput(ctx, top, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		originHead = strings.TrimSpace(string(out))
	}
	if headOut, err := gitOutput(ctx, top, "rev-parse", "--verify", "--end-of-options", "HEAD^{commit}"); err == nil {
		head := strings.TrimSpace(string(headOut))
		if headOnOrigin(ctx, top, head, upstream, originHead) {
			return linkRef{sha: head, label: "HEAD", atHead: true}, ""
		}
	}
	for _, candidate := range []string{upstream, originHead} {
		if candidate == "" {
			continue
		}
		out, err := gitOutput(ctx, top, "rev-parse", "--verify", "--end-of-options", candidate+"^{commit}")
		if err == nil {
			return linkRef{sha: strings.TrimSpace(string(out)), label: strings.TrimPrefix(candidate, "refs/remotes/")}, ""
		}
	}
	return linkRef{}, "this checkout's commit is not on origin, and it has no origin upstream or origin/HEAD"
}

func headOnOrigin(ctx context.Context, top, head, upstream, originHead string) bool {
	key := top + "\x00" + head
	if cachedOnOrigin(key) {
		return true
	}
	proved := false
	for _, candidate := range []string{upstream, originHead} {
		if candidate == "" {
			continue
		}
		if code, err := gitExitCode(ctx, top, "merge-base", "--is-ancestor", head, candidate); err == nil && code == 0 {
			proved = true
			break
		}
	}
	if !proved {
		out, err := gitOutput(ctx, top, "for-each-ref", "--count=1", "--contains", head, "--format=%(refname)", "refs/remotes/origin/")
		proved = err == nil && strings.TrimSpace(string(out)) != ""
	}
	if proved {
		rememberOnOrigin(key)
	}
	return proved
}

// pathUncommitted reports staged, unstaged, or untracked changes under
// pathspec relative to HEAD.
func pathUncommitted(ctx context.Context, top, pathspec string) (bool, error) {
	out, err := gitOutput(ctx, top, "status", "--porcelain=v2", "-z", "--untracked-files=normal", "--ignore-submodules=all", "--", pathspec)
	if err != nil {
		return false, err
	}
	return len(out) > 0, nil
}

// gitExitCode runs a Git command whose answer is its exit status: 0 or 1 are
// answers, anything else is an error.
func gitExitCode(ctx context.Context, cwd string, args ...string) (int, error) {
	_, err := gitOutputWithExit(ctx, cwd, false, args...)
	if err == nil {
		return 0, nil
	}
	if code := exitCode(err); code == 1 && ctx.Err() == nil {
		return 1, nil
	}
	return -1, err
}
