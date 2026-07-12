// Package resolve implements mission context resolution.
package resolve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mish/internal/missionfs"
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
	RefusalMissionNotFound  RefusalKind = "mission_not_found"
	RefusalNoContext        RefusalKind = "no_context"
	RefusalMultipleMarkers  RefusalKind = "multiple_markers"
	RefusalRepoUnset        RefusalKind = "missions_repo_unset"
	RefusalMarkerMissing    RefusalKind = "marker_points_at_missing_mission"
	RefusalInvalidSlug      RefusalKind = "invalid_slug"
	RefusalMarkerUnreadable RefusalKind = "marker_unreadable"
)

type FS interface {
	Stat(name string) (fs.FileInfo, error)
	ReadFile(name string) ([]byte, error)
}

type EnvLookup func(string) string

type Options struct {
	MissionFlagSet bool
	MissionFlag    string
	// CWD is the caller-supplied working directory. It is required unless
	// MissionFlag is set; Resolve never falls back to ambient process cwd.
	CWD string
	Env EnvLookup
	FS  FS
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

	if opts.MissionFlagSet || opts.MissionFlag != "" {
		if err := validateSlug(opts.MissionFlag); err != nil {
			return Result{}, err
		}
		repo := env(missionsRepoEnv)
		if repo == "" {
			return Result{}, repoUnset()
		}
		return resolveSlug(fsys, repo, opts.MissionFlag, SourceFlag, "")
	}

	if opts.CWD == "" {
		return Result{}, noContext()
	}

	if result, ok, err := resolveFromCWD(fsys, cwd); err != nil {
		return Result{}, err
	} else if ok {
		return result, nil
	}

	markers, err := collectMarkers(fsys, cwd)
	if err != nil {
		return Result{}, err
	}
	switch len(markers) {
	case 0:
		return Result{}, noContext()
	case 1:
		if err := validateSlug(markers[0].slug); err != nil {
			return Result{}, err
		}
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

func resolveFromCWD(fsys FS, cwd string) (Result, bool, error) {
	for _, dir := range ancestors(cwd) {
		if !fileExists(fsys, filepath.Join(dir, "mission.md")) {
			continue
		}
		parent := filepath.Dir(dir)
		if filepath.Base(parent) != "missions" {
			continue
		}
		slug := filepath.Base(dir)
		if err := validateSlug(slug); err != nil {
			return Result{}, false, err
		}
		return Result{
			Slug:       slug,
			MissionDir: dir,
			Source:     SourceCWD,
		}, true, nil
	}
	return Result{}, false, nil
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
			return nil, &Refusal{
				Kind:   RefusalMarkerUnreadable,
				Paths:  []string{path},
				Reason: fmt.Sprintf("could not read .mission marker %s", path),
				Remedy: "fix marker permissions or remove the marker",
			}
		}
		firstLine, _, _ := strings.Cut(string(data), "\n")
		firstLine = strings.TrimSpace(firstLine)
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

func noContext() error {
	return &Refusal{
		Kind:   RefusalNoContext,
		Reason: "no mission context found",
		Remedy: "pass --mission <slug>, run from inside missions/<slug>/, or add a .mission marker",
	}
}

func validateSlug(slug string) error {
	if err := missionfs.ValidateSlug(slug); err == nil {
		return nil
	} else {
		return &Refusal{
			Kind:   RefusalInvalidSlug,
			Slug:   slug,
			Reason: fmt.Sprintf("invalid mission slug %q: %v", slug, err),
			Remedy: "use lowercase letters, digits, and single hyphens, with no trailing hyphen",
		}
	}
}

func IsRefusal(err error, kind RefusalKind) bool {
	var refusal *Refusal
	return errors.As(err, &refusal) && refusal.Kind == kind
}
