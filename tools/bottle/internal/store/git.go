// Git substrate: the store root is a lazily-initialized git repository and
// every mutation auto-commits. Git here is a dumb substrate, not the data
// model — meta.json owns provenance, the registry owns resolution. Every
// operation works without git: the tool warns once and skips the commit
// step (best-effort), and a later mutation with git available sweeps the
// untracked prior state into its first commit via `git add -A`.

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const configFile = "config.json"

// storeConfig is the optional per-store-location config.json. Its one knob
// today turns the git substrate off, for stores on network mounts or
// remotely-versioned storage where a local git layer is redundant.
type storeConfig struct {
	GitAutoCommit *bool `json:"git_auto_commit"`
}

func (c storeConfig) gitEnabled() bool {
	return c.GitAutoCommit == nil || *c.GitAutoCommit
}

func (s *Store) loadConfig() error {
	raw, err := s.backend.Read(configFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, &s.config)
}

// autoCommit commits the whole store after a mutation. Best-effort by
// design: failures degrade to a warning, never to a failed operation. It
// runs under the mutation flock, so commits serialize with the writes they
// capture. Git is looked up per call, not cached, so a store that started
// life without git activates the substrate on the first mutation after git
// appears.
func (s *Store) autoCommit(msg string) {
	if !s.config.gitEnabled() {
		return
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		s.gitAbsentOnce.Do(func() {
			fmt.Fprintf(s.warn, "bottle: git not found — %s keeps no history until git is installed (operations continue without it)\n", s.root)
		})
		return
	}
	if _, err := os.Stat(filepath.Join(s.root, ".git")); os.IsNotExist(err) {
		// The flock is transient noise; keep it out of the history.
		if err := s.backend.Write(".gitignore", []byte(lockFile+"\n")); err != nil {
			s.warnf("git substrate: %v", err)
			return
		}
		if out, err := s.git(gitPath, "-c", "init.defaultBranch=main", "init", "-q"); err != nil {
			s.warnf("git init failed: %v: %s", err, out)
			return
		}
	}
	if out, err := s.git(gitPath, "add", "-A"); err != nil {
		s.warnf("git add failed: %v: %s", err, out)
		return
	}
	if _, err := s.git(gitPath, "diff", "--cached", "--quiet"); err == nil {
		return // nothing staged, nothing to commit
	}
	out, err := s.git(gitPath,
		"-c", "user.name=bottle", "-c", "user.email=bottle@localhost",
		"-c", "commit.gpgsign=false",
		"commit", "-q", "-m", msg)
	if err != nil {
		s.warnf("git commit failed: %v: %s", err, out)
	}
}

func (s *Store) git(gitPath string, args ...string) (string, error) {
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = s.root
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (s *Store) warnf(format string, args ...any) {
	fmt.Fprintf(s.warn, "bottle: "+format+"\n", args...)
}
