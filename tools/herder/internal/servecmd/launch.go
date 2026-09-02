package servecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type launchResponse struct {
	Names      []string `json:"names"`
	OutputTail string   `json:"output_tail"`
}

type launchEdge struct {
	Name     string    `json:"name"`
	Launcher string    `json:"launcher"`
	Tool     string    `json:"tool"`
	Model    string    `json:"model"`
	Effort   string    `json:"effort"`
	Tag      string    `json:"tag"`
	Repo     string    `json:"repo"`
	Time     time.Time `json:"time"`
}

var launchEdgeMu sync.Mutex

type launchBranchAllocator struct {
	mu       sync.Mutex
	reserved map[string]struct{}
}

func newLaunchBranchAllocator() *launchBranchAllocator {
	return &launchBranchAllocator{reserved: make(map[string]struct{})}
}

func (a *launchBranchAllocator) reserve(ctx context.Context, repo, base string, exists func(context.Context, string, string) (bool, error)) (string, func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		key := repo + "\x00" + candidate
		if _, ok := a.reserved[key]; ok {
			continue
		}
		found, err := exists(ctx, repo, candidate)
		if err != nil {
			return "", nil, err
		}
		if found {
			continue
		}
		a.reserved[key] = struct{}{}
		return candidate, func() {
			a.mu.Lock()
			delete(a.reserved, key)
			a.mu.Unlock()
		}, nil
	}
}

func localBranchExists(ctx context.Context, repo, branch string) (bool, error) {
	err := exec.CommandContext(ctx, "git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("inspect local branch %q in %q: %w", branch, repo, err)
}

func appendLaunchEdge(edge launchEdge) error {
	state, err := herderStateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	encoded, err := json.Marshal(edge)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	launchEdgeMu.Lock()
	defer launchEdgeMu.Unlock()
	file, err := os.OpenFile(filepath.Join(state, "launch-edges.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func herderStateDir() (string, error) {
	if state := strings.TrimSpace(os.Getenv("HERDER_STATE_DIR")); state != "" {
		return filepath.Clean(state), nil
	}
	if state := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); state != "" {
		return filepath.Join(state, "herder"), nil
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".local", "state", "herder"), nil
	}
	return "", errors.New("HERDER_STATE_DIR, XDG_STATE_HOME, and HOME are all unset")
}
