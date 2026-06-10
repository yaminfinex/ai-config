package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"ai-config/tools/bottle/internal/refs"
)

// cmdArtifacts lists a bottle's attached artifacts, or extracts them with
// --extract DIR (default ./bottle-artifacts/<name>@<v>/). Extraction never
// overwrites: if any target path already exists, it refuses up front and names
// the collision, writing nothing.
func cmdArtifacts(d *deps, args []string) int {
	fs := flag.NewFlagSet("artifacts", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	extract := fs.String("extract", "", "extract artifacts into DIR (default ./bottle-artifacts/<name>@<v>/)")
	extractSet := false
	pos, err := parseFlexibleTracking(fs, args, "extract", &extractSet)
	if err != nil {
		return 2
	}
	if len(pos) < 1 {
		fmt.Fprintln(d.stderr, "Usage: bottle artifacts <name>[@v] [--extract DIR]")
		return 2
	}
	ref, err := refs.Parse(pos[0])
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle artifacts: %v\n", err)
		return 1
	}
	b, err := d.store.Resolve(ref)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle artifacts: %v\n", err)
		return 1
	}
	names, err := d.store.Artifacts(b.ID)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle artifacts: %v\n", err)
		return 1
	}

	if !extractSet {
		if len(names) == 0 {
			fmt.Fprintf(d.stdout, "%s@%d has no artifacts.\n", b.Meta.Name, b.Meta.Version)
			return 0
		}
		for _, n := range names {
			fmt.Fprintln(d.stdout, n)
		}
		return 0
	}

	if len(names) == 0 {
		fmt.Fprintf(d.stdout, "%s@%d has no artifacts to extract.\n", b.Meta.Name, b.Meta.Version)
		return 0
	}

	dir := *extract
	if dir == "" {
		dir = filepath.Join("bottle-artifacts", fmt.Sprintf("%s@%d", b.Meta.Name, b.Meta.Version))
	}

	// Pre-scan: refuse before writing anything if any target already exists.
	for _, n := range names {
		target := filepath.Join(dir, filepath.FromSlash(n))
		if _, err := os.Lstat(target); err == nil {
			fmt.Fprintf(d.stderr, "bottle artifacts: refusing to overwrite existing file %s\n", target)
			return 1
		}
	}

	for _, n := range names {
		data, err := d.store.ReadArtifact(b.ID, n)
		if err != nil {
			fmt.Fprintf(d.stderr, "bottle artifacts: %v\n", err)
			return 1
		}
		target := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			fmt.Fprintf(d.stderr, "bottle artifacts: %v\n", err)
			return 1
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			fmt.Fprintf(d.stderr, "bottle artifacts: %v\n", err)
			return 1
		}
		fmt.Fprintf(d.stdout, "Extracted %s\n", target)
	}
	return 0
}

// parseFlexibleTracking is parseFlexible plus a record of whether a named flag
// was actually set (so artifacts can distinguish `--extract` with an empty/
// default value — list mode vs extract mode).
func parseFlexibleTracking(fs *flag.FlagSet, args []string, watch string, set *bool) ([]string, error) {
	pos, err := parseFlexible(fs, args)
	if err != nil {
		return nil, err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == watch {
			*set = true
		}
	})
	return pos, nil
}
