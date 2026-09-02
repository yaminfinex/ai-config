package servecmd

import (
	"encoding/json"
	"errors"
	"os"
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
	Tag      string    `json:"tag"`
	Repo     string    `json:"repo"`
	Time     time.Time `json:"time"`
}

var launchEdgeMu sync.Mutex

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
