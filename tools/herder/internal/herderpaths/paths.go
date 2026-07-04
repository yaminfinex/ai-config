package herderpaths

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	RepoRoot   string
	ScriptsDir string
	HerderSend string
	BinHerder  string
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
	return Paths{}, fmt.Errorf("could not locate ai-config root containing skills/herder/scripts/herder-send and bin/herder")
}

func pathsForRoot(root string) (Paths, bool) {
	absRoot, err := filepath.Abs(root)
	if err == nil {
		root = absRoot
	}
	scriptsDir := filepath.Join(root, "skills", "herder", "scripts")
	herderSend := filepath.Join(scriptsDir, "herder-send")
	binHerder := filepath.Join(root, "bin", "herder")
	if !isExecutableFile(herderSend) || !isExecutableFile(binHerder) {
		return Paths{}, false
	}
	return Paths{
		RepoRoot:   root,
		ScriptsDir: scriptsDir,
		HerderSend: herderSend,
		BinHerder:  binHerder,
	}, true
}

func isExecutableFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}
