// Package missioncontext resolves explicit and cwd-derived mission membership.
package missioncontext

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

const (
	SourceExplicit = "explicit"
	SourceCWD      = "cwd"
	SourceMarker   = "marker"
	maxSlugLength  = 64
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type Refusal struct {
	Kind   string
	Reason string
	Remedy string
}

func (r *Refusal) Error() string { return r.Reason }

type FS interface {
	Stat(name string) (fs.FileInfo, error)
	ReadFile(name string) ([]byte, error)
}

type OSFS struct{}

func (OSFS) Stat(name string) (fs.FileInfo, error) { return os.Stat(name) }
func (OSFS) ReadFile(name string) ([]byte, error)  { return os.ReadFile(name) }

type Options struct {
	CWD string
	Env func(string) string
	FS  FS
}

func ValidateSlug(slug string) error {
	var reason string
	switch {
	case slug == "":
		reason = "slug is required"
	case len(slug) > maxSlugLength:
		reason = fmt.Sprintf("slug must be at most %d characters", maxSlugLength)
	case strings.Contains(slug, "--"):
		reason = "slug must not contain consecutive hyphens"
	case strings.HasSuffix(slug, "-"):
		reason = "slug must not end with a hyphen"
	case !slugPattern.MatchString(slug):
		reason = "slug must start with a lowercase letter or digit and contain only lowercase letters, digits, and hyphens"
	default:
		return nil
	}
	return &Refusal{
		Kind:   "invalid_mission_slug",
		Reason: fmt.Sprintf("invalid mission slug %q: %s", slug, reason),
		Remedy: "use lowercase letters, digits, and single hyphens, with no trailing hyphen",
	}
}

func ResolveExplicit(slug string, opts Options) (v2.Mission, error) {
	if err := ValidateSlug(slug); err != nil {
		return v2.Mission{}, err
	}
	env, fsys := dependencies(opts)
	repo := env("MISSIONS_REPO")
	if repo == "" {
		return v2.Mission{}, &Refusal{
			Kind:   "missions_repo_unset",
			Reason: "$MISSIONS_REPO is not set",
			Remedy: "set MISSIONS_REPO to the shared missions repository",
		}
	}
	missionDir := filepath.Join(repo, "missions", slug)
	if !dirExists(fsys, missionDir) {
		return v2.Mission{}, &Refusal{
			Kind:   "mission_not_found",
			Reason: fmt.Sprintf("mission %s not found", slug),
			Remedy: "check the slug or create the mission",
		}
	}
	return v2.Mission{Slug: slug, Source: SourceExplicit}, nil
}

func ResolveCWD(opts Options) (v2.Mission, error) {
	if opts.CWD == "" {
		return noContext()
	}
	env, fsys := dependencies(opts)
	cwd := filepath.Clean(opts.CWD)
	for _, dir := range ancestors(cwd) {
		if !fileExists(fsys, filepath.Join(dir, "mission.md")) || filepath.Base(filepath.Dir(dir)) != "missions" {
			continue
		}
		slug := filepath.Base(dir)
		if err := ValidateSlug(slug); err != nil {
			return v2.Mission{}, err
		}
		return v2.Mission{Slug: slug, Source: SourceCWD}, nil
	}

	type marker struct{ path, slug string }
	var markers []marker
	for _, dir := range ancestors(cwd) {
		path := filepath.Join(dir, ".mission")
		if !fileExists(fsys, path) {
			continue
		}
		data, err := fsys.ReadFile(path)
		if err != nil {
			return v2.Mission{}, &Refusal{
				Kind:   "marker_unreadable",
				Reason: fmt.Sprintf("could not read .mission marker %s", path),
				Remedy: "fix marker permissions or remove the marker",
			}
		}
		first, _, _ := strings.Cut(string(data), "\n")
		markers = append(markers, marker{path: path, slug: strings.TrimSpace(first)})
	}
	if len(markers) == 0 {
		return noContext()
	}
	if len(markers) > 1 {
		paths := make([]string, len(markers))
		for i := range markers {
			paths[i] = markers[i].path
		}
		return v2.Mission{}, &Refusal{
			Kind:   "multiple_markers",
			Reason: "multiple .mission markers on ancestor chain: " + strings.Join(paths, ", "),
			Remedy: "remove nested markers so exactly one marker applies",
		}
	}
	selected := markers[0]
	if err := ValidateSlug(selected.slug); err != nil {
		return v2.Mission{}, err
	}
	repo := env("MISSIONS_REPO")
	if repo == "" {
		return v2.Mission{}, &Refusal{
			Kind:   "missions_repo_unset",
			Reason: "$MISSIONS_REPO is not set",
			Remedy: "set MISSIONS_REPO to the shared missions repository",
		}
	}
	if !dirExists(fsys, filepath.Join(repo, "missions", selected.slug)) {
		return v2.Mission{}, &Refusal{
			Kind:   "marker_points_at_missing_mission",
			Reason: fmt.Sprintf("marker points at missing mission %s", selected.slug),
			Remedy: "fix the .mission marker or create the mission",
		}
	}
	return v2.Mission{Slug: selected.slug, Source: SourceMarker}, nil
}

func dependencies(opts Options) (func(string) string, FS) {
	env := opts.Env
	if env == nil {
		env = os.Getenv
	}
	fsys := opts.FS
	if fsys == nil {
		fsys = OSFS{}
	}
	return env, fsys
}

func noContext() (v2.Mission, error) {
	return v2.Mission{}, &Refusal{
		Kind:   "no_context",
		Reason: "no mission context found",
		Remedy: "run from inside missions/<slug>/ or add a .mission marker",
	}
}

func ancestors(path string) []string {
	var out []string
	for dir := filepath.Clean(path); ; dir = filepath.Dir(dir) {
		out = append(out, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			return out
		}
	}
}

func fileExists(fsys FS, path string) bool {
	info, err := fsys.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(fsys FS, path string) bool {
	info, err := fsys.Stat(path)
	return err == nil && info.IsDir()
}
