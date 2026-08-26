package servecmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	watchSourceEnv   = "HERDER_WATCH_SOURCE_DIR"
	watchLauncherEnv = "HERDER_WATCH_LAUNCHER"
	watchPollCadence = 2 * time.Second
	watchStablePolls = 2
)

type watchTarget interface {
	Snapshot() (string, error)
	Description() string
}

type watchConfig struct {
	target   watchTarget
	execPath string
	argv     []string
	env      []string
	poll     time.Duration
	exec     func(string, []string, []string) error
}

type sourceWatchTarget struct{ root string }

func (t sourceWatchTarget) Description() string { return "source tree " + t.root }

// Snapshot hashes the same input set as bin/herder: module metadata, Go
// sources under cmd/internal, and every embedded web distribution asset.
func (t sourceWatchTarget) Snapshot() (string, error) {
	paths := make([]string, 0)
	for _, start := range []string{"go.mod", "cmd", "internal"} {
		root := filepath.Join(t.root, start)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(t.root, path)
			if err != nil {
				return err
			}
			if rel == "go.mod" || rel == "go.sum" || strings.HasSuffix(rel, ".go") || strings.HasPrefix(filepath.ToSlash(rel), "internal/webui/dist/") {
				paths = append(paths, rel)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, rel := range paths {
		content, err := os.ReadFile(filepath.Join(t.root, rel))
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(rel), len(content))
		_, _ = hash.Write(content)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type executableWatchTarget struct{ path string }

func (t executableWatchTarget) Description() string { return "executable " + t.path }

func (t executableWatchTarget) Snapshot() (string, error) {
	realPath, err := filepath.EvalSymlinks(t.path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return "", err
	}
	identity := fmt.Sprintf("path=%s size=%d mtime=%d", realPath, info.Size(), info.ModTime().UnixNano())
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		identity += fmt.Sprintf(" dev=%d ino=%d", stat.Dev, stat.Ino)
	}
	return identity, nil
}

func newWatchConfig(args []string) (watchConfig, error) {
	poll := watchPollCadence
	if source := os.Getenv(watchSourceEnv); source != "" {
		root, err := filepath.Abs(source)
		if err != nil {
			return watchConfig{}, fmt.Errorf("resolve watched source: %w", err)
		}
		launcher := os.Getenv(watchLauncherEnv)
		if launcher == "" {
			return watchConfig{}, fmt.Errorf("%s is set but %s is missing", watchSourceEnv, watchLauncherEnv)
		}
		launcher, err = filepath.Abs(launcher)
		if err != nil {
			return watchConfig{}, fmt.Errorf("resolve watch launcher: %w", err)
		}
		config := watchConfig{target: sourceWatchTarget{root: root}, execPath: launcher, argv: append([]string(nil), args...), env: os.Environ(), poll: poll, exec: syscall.Exec}
		if _, err := config.target.Snapshot(); err != nil {
			return watchConfig{}, fmt.Errorf("snapshot watched source: %w", err)
		}
		return config, nil
	}

	executable, err := os.Executable()
	if err != nil {
		return watchConfig{}, fmt.Errorf("resolve running executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return watchConfig{}, fmt.Errorf("resolve running executable: %w", err)
	}
	config := watchConfig{target: executableWatchTarget{path: executable}, execPath: executable, argv: append([]string(nil), args...), env: os.Environ(), poll: poll, exec: syscall.Exec}
	if _, err := config.target.Snapshot(); err != nil {
		return watchConfig{}, fmt.Errorf("snapshot running executable: %w", err)
	}
	return config, nil
}

func startWatch(ctx context.Context, config watchConfig, stderr io.Writer) {
	fmt.Fprintf(stderr, "herder serve: watch started on %s\n", config.target.Description())
	go func() {
		baseline, err := config.target.Snapshot()
		if err != nil {
			fmt.Fprintf(stderr, "herder serve: watch snapshot failed: %v\n", err)
			return
		}
		ticker := time.NewTicker(config.poll)
		defer ticker.Stop()
		pending := ""
		stable := 0
		detected := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, snapshotErr := config.target.Snapshot()
				if snapshotErr != nil {
					fmt.Fprintf(stderr, "herder serve: watch snapshot failed; retrying: %v\n", snapshotErr)
					pending, stable = "", 0
					continue
				}
				if current == baseline {
					pending, stable, detected = "", 0, false
					continue
				}
				if !detected {
					fmt.Fprintf(stderr, "herder serve: watch change detected in %s; waiting for it to settle\n", config.target.Description())
					detected = true
				}
				if current != pending {
					pending, stable = current, 1
					continue
				}
				stable++
				if stable < watchStablePolls {
					continue
				}
				fmt.Fprintf(stderr, "herder serve: watch re-exec via %s\n", config.execPath)
				if execErr := config.exec(config.execPath, config.argv, config.env); execErr != nil {
					fmt.Fprintf(stderr, "herder serve: watch re-exec failed; continuing current server: %v\n", execErr)
					baseline, pending, stable, detected = current, "", 0, false
				}
			}
		}
	}()
}
