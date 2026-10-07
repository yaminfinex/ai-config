package servecmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"ai-config/tools/herder/internal/backlogapi"
	"ai-config/tools/herder/internal/fileapi"
	"ai-config/tools/herder/internal/fileresolver"
	"ai-config/tools/herder/internal/fileroots"
	"ai-config/tools/herder/internal/hcomidentity"
)

type resolveResponse struct {
	Candidates []fileresolver.Result      `json:"candidates"`
	Roots      []fileresolver.RootOutcome `json:"roots"`
	Total      int                        `json:"total"`
}

const (
	// DefaultResolveLimit is how many ranked candidates /api/resolve returns
	// without a limit: more than a handful means the query needs narrowing.
	DefaultResolveLimit = 10
	MaxResolveLimit     = 100
)

func resolveLimit(r *http.Request) (int, error) {
	raw, err := optionalQuery(r, "limit")
	if err != nil {
		return 0, err
	}
	if _, present := r.URL.Query()["limit"]; !present {
		return DefaultResolveLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > MaxResolveLimit {
		return 0, fmt.Errorf("query parameter \"limit\" must be an integer from 1 to %d", MaxResolveLimit)
	}
	return limit, nil
}

func buildRootSet(ctx context.Context, configured []string, rows []hcomidentity.Row) (fileroots.Set, error) {
	agents := make([]fileroots.Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, fileroots.Agent{Name: row.Name, CWD: row.Directory})
	}
	return fileroots.Build(ctx, configured, agents)
}

func serveResolve(w http.ResponseWriter, r *http.Request, deps dependencies) {
	query, err := requiredQuery(r, "q")
	if err != nil || fileresolver.NormalizeQuery(query).Path == "" {
		if err == nil {
			err = errors.New("q must normalize to a non-empty path")
		}
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	agent, err := optionalQuery(r, "agent")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	root, rootErr := optionalQuery(r, "root")
	path, pathErr := optionalQuery(r, "path")
	if rootErr != nil || pathErr != nil {
		refuse(w, http.StatusBadRequest, "bad request", errors.Join(rootErr, pathErr).Error())
		return
	}
	_, rootPresent := r.URL.Query()["root"]
	_, pathPresent := r.URL.Query()["path"]
	_, agentPresent := r.URL.Query()["agent"]
	if rootPresent != pathPresent || rootPresent && (root == "" || path == "") {
		refuse(w, http.StatusBadRequest, "bad request", "query parameters \"root\" and \"path\" must be non-empty and appear together")
		return
	}
	if agentPresent && rootPresent {
		refuse(w, http.StatusBadRequest, "bad request", "query parameter \"agent\" cannot be combined with \"root\" and \"path\"")
		return
	}
	cleanPath := filepath.Clean(path)
	if rootPresent && (filepath.IsAbs(path) || cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator))) {
		refuse(w, http.StatusBadRequest, "bad request", fmt.Sprintf("query parameter \"path\" must stay relative to root: %q", path))
		return
	}
	limit, err := resolveLimit(r)
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	set, rows, err := deps.fleet().RootsAccepting(r.Context(), func(set fileroots.Set, rows []hcomidentity.Row) bool {
		if rootPresent && !set.Contains(root) {
			return false
		}
		return agent == "" || slices.ContainsFunc(rows, func(row hcomidentity.Row) bool { return row.Name == agent })
	})
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if agent != "" {
		found := false
		for _, row := range rows {
			if row.Name == agent {
				found = true
				break
			}
		}
		if !found {
			refuse(w, http.StatusNotFound, "unknown agent", fmt.Sprintf("no live bus agent named %q", agent))
			return
		}
	}
	// An absolute query ignores any file context: one that exists opens
	// directly; one that does not is scoped to the single most specific live
	// root, never the others. A context root (possibly a direct-open root
	// outside the live set) therefore need not be live for it.
	if normalized := fileresolver.NormalizeQuery(query).Path; filepath.IsAbs(normalized) {
		if response, handled := directOpen(r.Context(), normalized); handled {
			writeJSON(w, http.StatusOK, response)
			return
		}
		scoped, ok := set.MostSpecific(normalized)
		if !ok {
			writeJSON(w, http.StatusOK, resolveResponse{Candidates: []fileresolver.Result{}, Roots: []fileresolver.RootOutcome{}, Total: 0})
			return
		}
		writeResolution(w, deps, r, query, []string{scoped}, []string{scoped}, nil, limit)
		return
	}
	var anchor *fileresolver.Anchor
	if rootPresent {
		if !set.Contains(root) {
			refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe", root))
			return
		}
		anchor = &fileresolver.Anchor{Root: root, Path: cleanPath}
	}
	writeResolution(w, deps, r, query, set.Roots, set.Preference(agent), anchor, limit)
}

func writeResolution(w http.ResponseWriter, deps dependencies, r *http.Request, query string, roots, preference []string, anchor *fileresolver.Anchor, limit int) {
	resolution, err := deps.fileResolver.ResolveDetailed(r.Context(), fileresolver.Request{
		Query: query, Roots: roots, RootPreference: preference, Anchor: anchor,
	})
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if resolution.Results == nil {
		resolution.Results = []fileresolver.Result{}
	}
	if resolution.Roots == nil {
		resolution.Roots = []fileresolver.RootOutcome{}
	}
	// The cap applies after the resolver's ranking and dedupe; total is the
	// match count before it.
	total := len(resolution.Results)
	if total > limit {
		resolution.Results = resolution.Results[:limit]
	}
	writeJSON(w, http.StatusOK, resolveResponse{Candidates: resolution.Results, Roots: resolution.Roots, Total: total})
}

func serveFile(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, path, ok := fileQueries(w, r, false)
	if !ok {
		return
	}
	set, err := deps.fleet().RootsHolding(r.Context(), root, true)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if !set.Contains(root) && !directOpenRoot(root) {
		refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe nor an existing absolute directory", root))
		return
	}
	result, err := fileapi.Read(root, path, deps.now)
	if err != nil {
		serveFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func serveFileRaw(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, path, ok := fileQueries(w, r, false)
	if !ok {
		return
	}
	set, err := deps.fleet().RootsHolding(r.Context(), root, true)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if !set.Contains(root) && !directOpenRoot(root) {
		refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe nor an existing absolute directory", root))
		return
	}
	content, _, err := fileapi.ReadRaw(root, path)
	if err != nil {
		serveFileError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// serveFileImage streams an allowed image under the same root universe and
// file law as serveFileRaw. It is a separate endpoint so raw keeps its single
// text/plain contract and 4 MiB cap; the type here comes from the bytes.
func serveFileImage(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, path, ok := fileQueries(w, r, false)
	if !ok {
		return
	}
	set, err := deps.fleet().RootsHolding(r.Context(), root, true)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if !set.Contains(root) && !directOpenRoot(root) {
		refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe nor an existing absolute directory", root))
		return
	}
	file, info, mime, err := fileapi.OpenImage(root, path)
	if err != nil {
		serveFileError(w, err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if mime == "image/svg+xml" {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.CopyN(w, file, info.Size())
}

func serveTree(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, path, ok := fileQueries(w, r, true)
	if !ok {
		return
	}
	set, err := deps.fleet().RootsHolding(r.Context(), root, true)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if !set.Contains(root) && !directOpenRoot(root) {
		refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe nor an existing absolute directory", root))
		return
	}
	result, err := fileapi.Tree(root, path)
	if err != nil {
		serveFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func serveBacklog(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, err := requiredQuery(r, "root")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	path, err := requiredDirectoryQuery(r, "path")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	set, err := deps.fleet().RootsHolding(r.Context(), root, true)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if !set.Contains(root) && !directOpenRoot(root) {
		refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe nor an existing absolute directory", root))
		return
	}
	result, err := backlogapi.Read(root, path, deps.now)
	if err != nil {
		serveFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func fileQueries(w http.ResponseWriter, r *http.Request, tree bool) (string, string, bool) {
	root, err := requiredQuery(r, "root")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return "", "", false
	}
	var path string
	if tree {
		path, err = optionalQuery(r, "path")
	} else {
		path, err = requiredQuery(r, "path")
	}
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return "", "", false
	}
	return root, path, true
}

func requiredQuery(r *http.Request, name string) (string, error) {
	values, present := r.URL.Query()[name]
	if !present || len(values) != 1 || values[0] == "" {
		return "", fmt.Errorf("query parameter %q is required exactly once", name)
	}
	return values[0], nil
}

func optionalQuery(r *http.Request, name string) (string, error) {
	values, present := r.URL.Query()[name]
	if !present {
		return "", nil
	}
	if len(values) != 1 {
		return "", fmt.Errorf("query parameter %q may appear at most once", name)
	}
	return values[0], nil
}

func requiredDirectoryQuery(r *http.Request, name string) (string, error) {
	values, present := r.URL.Query()[name]
	if !present || len(values) != 1 {
		return "", fmt.Errorf("query parameter %q is required exactly once", name)
	}
	return values[0], nil
}

func serveFileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fileapi.ErrNotFound):
		refuse(w, http.StatusNotFound, "not found", err.Error())
	case errors.Is(err, fileapi.ErrRefused):
		refuse(w, http.StatusConflict, "refused by substrate", err.Error())
	default:
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
	}
}
