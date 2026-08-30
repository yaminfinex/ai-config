package servecmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"ai-config/tools/herder/internal/backlogapi"
	"ai-config/tools/herder/internal/fileapi"
	"ai-config/tools/herder/internal/fileresolver"
	"ai-config/tools/herder/internal/fileroots"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomidentity"
)

type resolveResponse struct {
	Candidates []fileresolver.Result      `json:"candidates"`
	Roots      []fileresolver.RootOutcome `json:"roots"`
}

func buildRootSet(ctx context.Context, configured []string, rows []hcomidentity.Row) (fileroots.Set, error) {
	agents := make([]fileroots.Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, fileroots.Agent{Name: row.Name, CWD: row.Directory})
	}
	return fileroots.Build(ctx, configured, agents)
}

func liveRootSet(ctx context.Context, deps dependencies) (fileroots.Set, []hcomidentity.Row, error) {
	rows, err := deps.roster()
	if err != nil {
		return fileroots.Set{}, nil, sourceError{"hcom", err}
	}
	if err := fleetview.ValidateRoster(rows); err != nil {
		return fileroots.Set{}, nil, sourceError{"hcom", fmt.Errorf("invalid roster: %w", err)}
	}
	set, err := deps.roots(ctx, deps.configuredRoots, rows)
	if err != nil {
		return fileroots.Set{}, nil, sourceError{"filesystem", err}
	}
	return set, rows, nil
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
	set, rows, err := liveRootSet(r.Context(), deps)
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
	var anchor *fileresolver.Anchor
	if rootPresent {
		if !set.Contains(root) {
			refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe", root))
			return
		}
		anchor = &fileresolver.Anchor{Root: root, Path: cleanPath}
	}
	resolution, err := deps.fileResolver.ResolveDetailed(r.Context(), fileresolver.Request{
		Query: query, Roots: set.Roots, RootPreference: set.Preference(agent), Anchor: anchor,
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
	writeJSON(w, http.StatusOK, resolveResponse{Candidates: resolution.Results, Roots: resolution.Roots})
}

func serveFile(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, path, ok := fileQueries(w, r, false)
	if !ok {
		return
	}
	set, _, err := liveRootSet(r.Context(), deps)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if !set.Contains(root) {
		refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe", root))
		return
	}
	result, err := fileapi.Read(root, path, deps.now)
	if err != nil {
		serveFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func serveTree(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, path, ok := fileQueries(w, r, true)
	if !ok {
		return
	}
	set, _, err := liveRootSet(r.Context(), deps)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if !set.Contains(root) {
		refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe", root))
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
	set, _, err := liveRootSet(r.Context(), deps)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return
	}
	if !set.Contains(root) {
		refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe", root))
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
