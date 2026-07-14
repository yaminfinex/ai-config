package launchcmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type grokInspectResult struct {
	MCPServers []struct {
		Name string `json:"name"`
	} `json:"mcpServers"`
	ConfigSources struct {
		Layers []struct {
			Role string `json:"role"`
			Path string `json:"path"`
		} `json:"layers"`
	} `json:"configSources"`
}

// This exercises the real vendor's resolved-config surface. It proves that the
// interactive entrypoint's project configuration is discoverable, but it does
// not claim that an authenticated TUI session activated the MCP subprocess.
func TestRealGrokResolvesProjectMCPFromEffectiveCWD(t *testing.T) {
	vendor, err := resolveGrokBinary()
	if err != nil {
		t.Skipf("real Grok vendor binary unavailable: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	seatCWD := filepath.Join(root, "seat-worktree")
	otherCWD := filepath.Join(root, "other-worktree")
	for _, dir := range []string{home, seatCWD, otherCWD} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	configPath, err := ensureGrokProjectMCPConfig(seatCWD)
	if err != nil {
		t.Fatal(err)
	}
	userConfig := filepath.Join(home, ".grok", "config.toml")
	assertAbsent(t, userConfig)

	inspect := func(args ...string) grokInspectResult {
		t.Helper()
		cmd := exec.Command(vendor, args...)
		cmd.Dir = seatCWD
		cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("real Grok inspect failed: %v output=%s", err, output)
		}
		var result grokInspectResult
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("decode real Grok inspect output: %v output=%s", err, output)
		}
		return result
	}

	resolved := inspect("inspect", "--json")
	if !inspectHasMCP(resolved, "hcom") {
		t.Fatalf("real Grok did not resolve hcom from project config: %+v", resolved)
	}
	if !inspectHasProjectLayer(resolved, configPath) {
		t.Fatalf("real Grok did not report the cwd-local project layer: %+v", resolved.ConfigSources)
	}
	assertAbsent(t, userConfig)
	t.Logf("real Grok resolved hcom from project layer %s with user config absent", configPath)

	// The server-level source.path in 0.2.99 can report the nonexistent user
	// path. configSources.layers is the authoritative provenance asserted here.
	divergent := inspect("--cwd", otherCWD, "inspect", "--json")
	if len(divergent.ConfigSources.Layers) != 0 || inspectHasMCP(divergent, "hcom") {
		t.Fatalf("real Grok loaded seat project config after effective cwd diverged: %+v", divergent)
	}
	assertAbsent(t, userConfig)
	t.Log("real Grok resolved zero project layers after --cwd moved away from the config directory")
}

func inspectHasMCP(result grokInspectResult, name string) bool {
	for _, server := range result.MCPServers {
		if server.Name == name {
			return true
		}
	}
	return false
}

func inspectHasProjectLayer(result grokInspectResult, path string) bool {
	for _, layer := range result.ConfigSources.Layers {
		if layer.Role == "project" && filepath.Clean(layer.Path) == filepath.Clean(path) {
			return true
		}
	}
	return false
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to remain absent, stat error=%v", path, err)
	}
}
