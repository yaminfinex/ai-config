// Package webaction wraps the existing fleet and hcom lifecycle commands.
package webaction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ai-config/tools/herder/internal/hcomcli"
)

const Timeout = 150 * time.Second

var commandTimeout = Timeout

var ErrUnavailable = errors.New("lifecycle substrate unavailable")

type Result struct {
	Name string `json:"name"`
	Pane string `json:"pane"`
}

func Spawn(ctx context.Context, args []string) (Result, error) {
	root := strings.TrimSpace(os.Getenv("AI_CONFIG_ROOT"))
	if root == "" {
		return Result{}, fmt.Errorf("%w: AI_CONFIG_ROOT is not set; cannot locate tools/fleet/spawn.sh", ErrUnavailable)
	}
	script := filepath.Join(root, "tools", "fleet", "spawn.sh")
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, script, args...)
	cmd.Env = hcomcli.AnonymousEnv()
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if commandCtx.Err() != nil {
		return Result{}, fmt.Errorf("%w: fleet spawn timed out after %s", ErrUnavailable, commandTimeout)
	}
	if err != nil {
		if unavailable(err) {
			return Result{}, fmt.Errorf("%w: run %s: %v", ErrUnavailable, script, err)
		}
		return Result{}, errors.New(detail(out, stderr.Bytes(), err))
	}
	result, err := parseSpawn(out)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v; output: %s", ErrUnavailable, err, strings.TrimSpace(string(out)))
	}
	return result, nil
}

func Fork(ctx context.Context, target, prompt string, hasPrompt bool) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	args := []string{"f", target}
	if hasPrompt {
		args = append(args, "--hcom-prompt", prompt)
	}
	args = append(args, "--go")
	cmd := hcomcli.CommandContext(commandCtx, args...)
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if commandCtx.Err() != nil {
		return "", fmt.Errorf("%w: hcom fork timed out after %s", ErrUnavailable, commandTimeout)
	}
	if err != nil {
		if unavailable(err) {
			return "", fmt.Errorf("%w: run hcom fork: %v", ErrUnavailable, err)
		}
		return "", errors.New(detail(out, stderr.Bytes(), err))
	}
	name, err := parseNames(out)
	if err != nil {
		return "", fmt.Errorf("%w: %v; output: %s", ErrUnavailable, err, strings.TrimSpace(string(out)))
	}
	return name, nil
}

func parseSpawn(out []byte) (Result, error) {
	var result Result
	var names, panes int
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "name":
			names++
			result.Name = value
		case "pane":
			panes++
			result.Pane = value
		}
	}
	if names != 1 || result.Name == "" || panes > 1 || (panes == 1 && result.Pane == "") {
		return Result{}, errors.New("fleet spawn output must contain exactly one name and at most one nonempty pane")
	}
	return result, nil
}

func parseNames(out []byte) (string, error) {
	for _, line := range strings.Split(string(out), "\n") {
		if names, ok := strings.CutPrefix(strings.TrimSpace(line), "Names:"); ok {
			fields := strings.Fields(names)
			if len(fields) == 1 {
				return fields[0], nil
			}
			return "", errors.New("hcom fork did not report exactly one launched name")
		}
	}
	return "", errors.New("hcom fork output omitted Names")
}

func unavailable(err error) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr) || errors.Is(err, exec.ErrNotFound)
}

func detail(stdout, stderr []byte, err error) string {
	if trimmed := bytes.TrimSpace(stderr); len(trimmed) > 0 {
		return string(trimmed)
	}
	if trimmed := bytes.TrimSpace(stdout); len(trimmed) > 0 {
		return string(trimmed)
	}
	return err.Error()
}
