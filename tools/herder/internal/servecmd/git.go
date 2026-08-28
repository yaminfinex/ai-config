package servecmd

import (
	"errors"
	"fmt"
	"net/http"

	"ai-config/tools/herder/internal/gitapi"
)

func serveGitStatus(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, ok := gitRoot(w, r, deps)
	if !ok {
		return
	}
	result, err := gitapi.ReadStatus(r.Context(), root, deps.now)
	if err != nil {
		serveGitError(w, err, "git unavailable")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func serveGitDiff(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, path, ok := gitPathQueries(w, r, deps)
	if !ok {
		return
	}
	base, err := requiredQuery(r, "base")
	if err != nil || base != "uncommitted" && base != "branch" {
		if err == nil {
			err = fmt.Errorf("query parameter %q must be exactly uncommitted or branch", "base")
		}
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	result, err := gitapi.ReadDiff(r.Context(), root, path, base, deps.now)
	if err != nil {
		serveGitError(w, err, "base unavailable")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func serveGitLog(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, path, ok := gitPathQueries(w, r, deps)
	if !ok {
		return
	}
	cursor, err := optionalQuery(r, "cursor")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	result, err := gitapi.ReadLog(r.Context(), root, path, cursor, deps.now)
	if err != nil {
		serveGitError(w, err, "git unavailable")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func serveGitFile(w http.ResponseWriter, r *http.Request, deps dependencies) {
	root, path, ok := gitPathQueries(w, r, deps)
	if !ok {
		return
	}
	sha, err := requiredQuery(r, "sha")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	result, err := gitapi.ReadFile(r.Context(), root, path, sha)
	if err != nil {
		serveGitError(w, err, "git unavailable")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writeJSON(w, http.StatusOK, result)
}

func gitPathQueries(w http.ResponseWriter, r *http.Request, deps dependencies) (string, string, bool) {
	root, ok := gitRoot(w, r, deps)
	if !ok {
		return "", "", false
	}
	path, err := requiredQuery(r, "path")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return "", "", false
	}
	return root, path, true
}

func gitRoot(w http.ResponseWriter, r *http.Request, deps dependencies) (string, bool) {
	root, err := requiredQuery(r, "root")
	if err != nil {
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
		return "", false
	}
	set, _, err := liveRootSet(r.Context(), deps)
	if err != nil {
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
		return "", false
	}
	if !set.Contains(root) {
		refuse(w, http.StatusNotFound, "unknown root", fmt.Sprintf("root %q is not in the live readable universe", root))
		return "", false
	}
	return root, true
}

func serveGitError(w http.ResponseWriter, err error, unavailableShort string) {
	switch {
	case errors.Is(err, gitapi.ErrBadRequest):
		refuse(w, http.StatusBadRequest, "bad request", err.Error())
	case errors.Is(err, gitapi.ErrNotFound):
		refuse(w, http.StatusNotFound, "not found", err.Error())
	case errors.Is(err, gitapi.ErrUnavailable):
		refuse(w, http.StatusConflict, unavailableShort, err.Error())
	case errors.Is(err, gitapi.ErrRefused):
		refuse(w, http.StatusConflict, "refused by substrate", err.Error())
	default:
		refuse(w, http.StatusBadGateway, "substrate unreachable", err.Error())
	}
}
