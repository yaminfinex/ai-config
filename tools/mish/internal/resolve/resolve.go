// Package resolve implements mission context resolution.
package resolve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const missionsRepoEnv = "MISSIONS_REPO"

type Source string

const (
	SourceFlag   Source = "flag"
	SourceCWD    Source = "cwd"
	SourceMarker Source = "marker"
)

type RefusalKind string

const (
	RefusalMissionNotFound RefusalKind = "mission_not_found"
	RefusalNoContext       RefusalKind = "no_context"
	RefusalMultipleMarkers RefusalKind = "multiple_markers"
	RefusalRepoUnset       RefusalKind = "missions_repo_unset"
	RefusalMarkerMissing   RefusalKind = "marker_points_at_missing_mission"
)

type FS interface {
	Stat(name string) (fs.FileInfo, error)
	ReadFile(name string) ([]byte, error)
}

type EnvLookup func(string) string

type Options struct {
	MissionFlag string
	CWD         string
	Env         EnvLookup
	FS          FS
}

type Result struct {
	Slug       string
	MissionDir string
	Source     Source
	MarkerPath string
}

type Refusal struct {
	Kind   RefusalKind
	Slug   string
	Paths  []string
	Reason string
	Remedy string
}

func (r *Refusal) Error() string {
	if r == nil {
		return ""
	}
	if r.Reason == "" {
		return string(r.Kind)
	}
	return r.Reason
}

type OSFS struct{}

func (OSFS) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(name)
}

func (OSFS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func Resolve(opts Options) (Result, error) {
	fsys := opts.FS
	if fsys == nil {
		fsys = OSFS{}
	}
	env := opts.Env
	if env == nil {
		env = os.Getenv
	}
	cwd := filepath.Clean(opts.CWD)
	if cwd == "." || cwd == "" {
		if actual, err := os.Getwd(); err == nil {
			cwd = actual
		}
	}

	if opts.MissionFlag != "" {
		repo := env(missionsRepoEnv)
		if repo == "" {
			return Result{}, repoUnset()
		}
		return resolveSlug(fsys, repo, opts.MissionFlag, SourceFlag, "")
	}

	if result, ok := resolveFromCWD(fsys, cwd); ok {
		return result, nil
	}

	markers, err := collectMarkers(fsys, cwd)
	if err != nil {
		return Result{}, err
	}
	switch len(markers) {
	case 0:
		return Result{}, &Refusal{
			Kind:   RefusalNoContext,
			Reason: "no mission context found",
			Remedy: "pass --mission <slug>, run from inside missions/<slug>/, or add a .mission marker",
		}
	case 1:
		repo := env(missionsRepoEnv)
		if repo == "" {
			return Result{}, repoUnset()
		}
		return resolveSlug(fsys, repo, markers[0].slug, SourceMarker, markers[0].path)
	default:
		paths := make([]string, 0, len(markers))
		for _, marker := range markers {
			paths = append(paths, marker.path)
		}
		return Result{}, &Refusal{
			Kind:   RefusalMultipleMarkers,
			Paths:  paths,
			Reason: fmt.Sprintf("multiple .mission markers on ancestor chain: %s", strings.Join(paths, ", ")),
			Remedy: "remove nested markers so exactly one marker applies",
		}
	}
}

func resolveSlug(fsys FS, repo, slug string, source Source, markerPath string) (Result, error) {
	missionDir := filepath.Join(repo, "missions", slug)
	if !dirExists(fsys, missionDir) {
		if source == SourceMarker {
			return Result{}, &Refusal{
				Kind:   RefusalMarkerMissing,
				Slug:   slug,
				Reason: fmt.Sprintf("marker points at missing mission %s", slug),
				Remedy: "fix the .mission marker or create the mission",
			}
		}
		return Result{}, &Refusal{
			Kind:   RefusalMissionNotFound,
			Slug:   slug,
			Reason: fmt.Sprintf("mission %s not found", slug),
			Remedy: "check the slug or create the mission",
		}
	}
	return Result{
		Slug:       slug,
		MissionDir: missionDir,
		Source:     source,
		MarkerPath: markerPath,
	}, nil
}

func resolveFromCWD(fsys FS, cwd string) (Result, bool) {
	for _, dir := range ancestors(cwd) {
		if !fileExists(fsys, filepath.Join(dir, "mission.md")) {
			continue
		}
		parent := filepath.Dir(dir)
		if filepath.Base(parent) != "missions" {
			continue
		}
		return Result{
			Slug:       filepath.Base(dir),
			MissionDir: dir,
			Source:     SourceCWD,
		}, true
	}
	return Result{}, false
}

type marker struct {
	path string
	slug string
}

func collectMarkers(fsys FS, cwd string) ([]marker, error) {
	var markers []marker
	for _, dir := range ancestors(cwd) {
		path := filepath.Join(dir, ".mission")
		if !fileExists(fsys, path) {
			continue
		}
		data, err := fsys.ReadFile(path)
		if err != nil {
			return nil, err
		}
		firstLine, _, _ := strings.Cut(string(data), "\n")
		firstLine = strings.TrimSuffix(firstLine, "\r")
		markers = append(markers, marker{path: path, slug: firstLine})
	}
	return markers, nil
}

func ancestors(path string) []string {
	clean := filepath.Clean(path)
	var dirs []string
	for {
		dirs = append(dirs, clean)
		parent := filepath.Dir(clean)
		if parent == clean {
			return dirs
		}
		clean = parent
	}
}

func dirExists(fsys FS, path string) bool {
	info, err := fsys.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(fsys FS, path string) bool {
	info, err := fsys.Stat(path)
	return err == nil && !info.IsDir()
}

func repoUnset() error {
	return &Refusal{
		Kind:   RefusalRepoUnset,
		Reason: "$MISSIONS_REPO is not set",
		Remedy: "set MISSIONS_REPO to the shared missions repo",
	}
}

func IsRefusal(err error, kind RefusalKind) bool {
	var refusal *Refusal
	return errors.As(err, &refusal) && refusal.Kind == kind
}
