package observerstatus

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const FileName = "observer.status.json"

type Status struct {
	Schema             string            `json:"schema"`
	Advice             bool              `json:"advice"`
	PID                int               `json:"pid,omitempty"`
	BuildHash          string            `json:"build_hash,omitempty"`
	HeartbeatAt        string            `json:"heartbeat_at,omitempty"`
	LastSweepAt        string            `json:"last_sweep_at,omitempty"`
	LastSweepSummary   Summary           `json:"last_sweep_summary"`
	ProtocolCompatible bool              `json:"protocol_compatible"`
	ProtocolDetail     string            `json:"protocol_detail,omitempty"`
	Flags              []Flag            `json:"flags,omitempty"`
	Confirmed          map[string]string `json:"confirmed,omitempty"`
}

type Summary struct {
	Applied int `json:"applied"`
	Noop    int `json:"noop"`
	Refused int `json:"refused"`
}

type Flag struct {
	GUID       string `json:"guid,omitempty"`
	Label      string `json:"label,omitempty"`
	Type       string `json:"type"`
	Severity   string `json:"severity,omitempty"`
	Detail     string `json:"detail"`
	Suggested  string `json:"suggested,omitempty"`
	TerminalID string `json:"terminal_id,omitempty"`
	PaneID     string `json:"pane_id,omitempty"`
}

func PathForStateDir(stateDir string) string {
	return filepath.Join(stateDir, FileName)
}

func DefaultPath() string {
	stateDir := os.Getenv("HERDER_STATE_DIR")
	if stateDir == "" {
		xdg := os.Getenv("XDG_STATE_HOME")
		if xdg == "" {
			home, _ := os.UserHomeDir()
			xdg = filepath.Join(home, ".local", "state")
		}
		stateDir = filepath.Join(xdg, "herder")
	}
	return PathForStateDir(stateDir)
}

func Read(path string) (Status, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Status{}, err
	}
	var st Status
	if err := json.Unmarshal(b, &st); err != nil {
		return Status{}, err
	}
	return st, nil
}

func WriteAtomic(path string, st Status) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".observer.status.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func Missing(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func FlagsByGUID(st Status) map[string][]Flag {
	out := map[string][]Flag{}
	for _, flag := range st.Flags {
		if flag.GUID == "" {
			continue
		}
		out[flag.GUID] = append(out[flag.GUID], flag)
	}
	return out
}
