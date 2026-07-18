package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mish/internal/missionfs"

	"github.com/spf13/cobra"
)

type newOptions struct {
	authority string
	owner     string
	title     string
	noMarker  bool
	text      bool
}

func newNewCommand(d deps) *cobra.Command {
	opts := newOptions{}
	cmd := &cobra.Command{
		Use:                "new <slug>",
		Short:              "Scaffold a mission directory",
		SilenceUsage:       true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if slicesContains(args, "--help") || slicesContains(args, "-h") {
				return cmd.Help()
			}
			parsed, slug, err := parseNewArgs(args)
			if err != nil {
				return err
			}
			opts = parsed
			return withRefusalText(runNew(d, opts, slug), opts.text)
		},
	}
	attachHelp(cmd, newHelpText)
	cmd.Flags().StringVar(&opts.authority, "authority", "", "manifest authority label")
	cmd.Flags().StringVar(&opts.owner, "owner", "", "human owner label")
	cmd.Flags().StringVar(&opts.title, "title", "", "mission title")
	cmd.Flags().BoolVar(&opts.noMarker, "no-marker", false, "do not write a .mission marker")
	return cmd
}

func parseNewArgs(args []string) (newOptions, string, error) {
	var opts newOptions
	var slugs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			slugs = append(slugs, args[i+1:]...)
			i = len(args)
		case arg == "--authority" || arg == "--owner" || arg == "--title":
			if i+1 >= len(args) {
				return newOptions{}, "", usageError{err: fmt.Errorf("mish new: %s requires a value", arg)}
			}
			i++
			assignNewOption(&opts, arg, args[i])
		case strings.HasPrefix(arg, "--authority="):
			opts.authority = strings.TrimPrefix(arg, "--authority=")
		case strings.HasPrefix(arg, "--owner="):
			opts.owner = strings.TrimPrefix(arg, "--owner=")
		case strings.HasPrefix(arg, "--title="):
			opts.title = strings.TrimPrefix(arg, "--title=")
		case arg == "--no-marker":
			opts.noMarker = true
		case arg == "--text":
			opts.text = true
		case strings.HasPrefix(arg, "--"):
			return newOptions{}, "", usageError{err: fmt.Errorf("mish new: unknown flag %s", arg)}
		default:
			slugs = append(slugs, arg)
		}
	}
	if len(slugs) != 1 {
		return newOptions{}, "", usageError{err: fmt.Errorf("mish new: expected exactly one slug")}
	}
	return opts, slugs[0], nil
}

func assignNewOption(opts *newOptions, flag, value string) {
	switch flag {
	case "--authority":
		opts.authority = value
	case "--owner":
		opts.owner = value
	case "--title":
		opts.title = value
	}
}

func slicesContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func runNew(d deps, opts newOptions, slug string) error {
	if d.missionsRepo == "" {
		return refusalError{
			verb:    "new",
			kind:    "missions_repo_unset",
			message: "$MISSIONS_REPO is not set",
			remedy:  "set MISSIONS_REPO to the shared missions repo before scaffolding a mission",
		}
	}
	if err := missionfs.ValidateSlug(slug); err != nil {
		return refusalError{
			verb:    "new",
			kind:    "invalid_slug",
			message: fmt.Sprintf("invalid slug %q: %v", slug, err),
			remedy:  "use lowercase letters, digits, and single hyphens",
		}
	}
	cwd, err := d.cwd()
	if err != nil {
		return refusalError{
			verb:    "new",
			kind:    "cwd_unavailable",
			message: fmt.Sprintf("could not determine current directory: %v", err),
			remedy:  "run from a readable working directory",
		}
	}

	markerAction, err := planMarkerWrite(cwd, d.missionsRepo, slug, opts.noMarker)
	if err != nil {
		return err
	}

	missionDir := filepath.Join(d.missionsRepo, "missions", slug)
	if info, err := os.Stat(missionDir); err == nil && info.IsDir() {
		return refusalError{
			verb:    "new",
			kind:    "mission_already_exists",
			slug:    slug,
			message: fmt.Sprintf("mission %s already exists", slug),
			remedy:  "choose a new slug or use the existing mission",
		}
	} else if err != nil && !os.IsNotExist(err) {
		return refusalError{
			verb:    "new",
			kind:    "mission_inspection_failed",
			slug:    slug,
			message: fmt.Sprintf("could not inspect mission %s: %v", slug, err),
			remedy:  "check permissions on $MISSIONS_REPO",
		}
	}

	authority, authoritySource := chooseAuthority(d, opts)
	owner, ownerSource := chooseOwner(d, opts)
	title := opts.title
	if title == "" {
		title = strings.ReplaceAll(slug, "-", " ")
	}
	created := d.clock().Format("2006-01-02")

	if err := os.MkdirAll(missionDir, 0o755); err != nil {
		return scaffoldRefusal("create mission directory", err)
	}
	manifest := missionfs.Manifest{
		Mission:   slug,
		Authority: authority,
		Owner:     owner,
		Status:    "active",
		Created:   created,
	}
	if err := missionfs.WriteManifest(filepath.Join(missionDir, "mission.md"), manifest, title); err != nil {
		return scaffoldRefusal("write mission.md", err)
	}
	if err := missionfs.WriteBoard(filepath.Join(missionDir, "backlog"), slug); err != nil {
		return scaffoldRefusal("write backlog board", err)
	}
	if err := missionfs.WriteAsksScaffold(missionDir); err != nil {
		return scaffoldRefusal("write asks board", err)
	}
	artifactsDir := filepath.Join(missionDir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return scaffoldRefusal("create artifacts directory", err)
	}
	if err := os.WriteFile(filepath.Join(artifactsDir, ".gitkeep"), nil, 0o644); err != nil {
		return scaffoldRefusal("write artifacts keep-file", err)
	}
	if markerAction.write {
		if err := os.WriteFile(filepath.Join(cwd, ".mission"), []byte(slug+"\n"), 0o644); err != nil {
			return scaffoldRefusal("write .mission marker", err)
		}
	}

	if opts.text {
		fmt.Fprintf(d.stdout, "created mission %s\n", slug)
		fmt.Fprintf(d.stdout, "authority: %s (source: %s)\n", authority, authoritySource)
		fmt.Fprintf(d.stdout, "owner: %s (source: %s)\n", owner, ownerSource)
	} else {
		markerPath := ""
		if markerAction.write {
			markerPath = filepath.Join(cwd, ".mission")
		}
		emitJSON(d.stdout, newOutput{
			OK: true, Slug: slug, MissionDir: missionDir, Manifest: manifest,
			AuthoritySource: authoritySource, OwnerSource: ownerSource, MarkerPath: markerPath,
		})
	}
	return nil
}

func scaffoldRefusal(action string, err error) error {
	return refusalError{
		verb: "new", kind: "mission_scaffold_failed",
		message: fmt.Sprintf("could not %s: %v", action, err),
		remedy:  "fix filesystem permissions, remove any partial scaffold, and retry",
	}
}

type newOutput struct {
	OK              bool               `json:"ok"`
	Slug            string             `json:"slug"`
	MissionDir      string             `json:"mission_dir"`
	Manifest        missionfs.Manifest `json:"manifest"`
	AuthoritySource string             `json:"authority_source"`
	OwnerSource     string             `json:"owner_source"`
	MarkerPath      string             `json:"marker_path,omitempty"`
}

func chooseAuthority(d deps, opts newOptions) (string, string) {
	if opts.authority != "" {
		return opts.authority, "flag"
	}
	return d.osUser, "OS user"
}

func chooseOwner(d deps, opts newOptions) (string, string) {
	if opts.owner != "" {
		return opts.owner, "flag"
	}
	if owner := d.env("SESSION_OWNER"); owner != "" {
		return owner, "env"
	}
	return d.osUser, "OS user"
}

type markerPlan struct {
	write bool
}

func planMarkerWrite(cwd, missionsRepo, slug string, noMarker bool) (markerPlan, error) {
	if noMarker {
		return markerPlan{write: false}, nil
	}
	sawSameSlugMarker := false
	for _, dir := range ancestors(cwd) {
		path := filepath.Join(dir, ".mission")
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return markerPlan{}, refusalError{
				verb:    "new",
				kind:    "marker_unreadable",
				paths:   []string{path},
				message: fmt.Sprintf("could not read .mission marker %s", path),
				remedy:  "fix marker permissions or remove the marker",
			}
		}
		firstLine, _, _ := strings.Cut(string(data), "\n")
		markerSlug := strings.TrimSpace(firstLine)
		if markerSlug != slug {
			return markerPlan{}, refusalError{
				verb:    "new",
				kind:    "marker_conflict",
				paths:   []string{path},
				message: fmt.Sprintf(".mission marker %s names %s, not %s", path, markerSlug, slug),
				remedy:  "remove the marker or pass --no-marker",
			}
		}
		sawSameSlugMarker = true
	}
	if sawSameSlugMarker {
		return markerPlan{write: false}, nil
	}
	if pathInside(cwd, missionsRepo) {
		return markerPlan{write: false}, nil
	}
	return markerPlan{write: true}, nil
}

func ancestors(path string) []string {
	clean := filepath.Clean(path)
	var out []string
	for {
		out = append(out, clean)
		parent := filepath.Dir(clean)
		if parent == clean {
			return out
		}
		clean = parent
	}
}

func pathInside(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
