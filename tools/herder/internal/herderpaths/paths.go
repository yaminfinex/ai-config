package herderpaths

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	RepoRoot  string
	BinHerder string
	ShimsDir  string
}

func Resolve() (Paths, error) {
	if root := os.Getenv("AI_CONFIG_ROOT"); root != "" {
		if paths, ok := pathsForRoot(root); ok {
			return paths, nil
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return Paths{}, err
	}
	for {
		if paths, ok := pathsForRoot(wd); ok {
			return paths, nil
		}
		next := filepath.Dir(wd)
		if next == wd {
			break
		}
		wd = next
	}
	return Paths{}, fmt.Errorf("could not locate ai-config root containing bin/herder and tools/herder/shims")
}

func pathsForRoot(root string) (Paths, bool) {
	absRoot, err := filepath.Abs(root)
	if err == nil {
		root = absRoot
	}
	binHerder := filepath.Join(root, "bin", "herder")
	shimsDir := filepath.Join(root, "tools", "herder", "shims")
	if !isExecutableFile(binHerder) || !isDir(shimsDir) {
		return Paths{}, false
	}
	return Paths{
		RepoRoot:  root,
		BinHerder: binHerder,
		ShimsDir:  shimsDir,
	}, true
}

func isExecutableFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
