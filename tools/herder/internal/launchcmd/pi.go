package launchcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

const piVendorPackage = "@earendil-works/pi-coding-agent"

var piProviderCredentials = []struct {
	provider string
	key      string
}{
	{provider: "anthropic", key: "ANTHROPIC_API_KEY"},
	{provider: "openai", key: "OPENAI_API_KEY"},
	{provider: "xai", key: "XAI_API_KEY"},
}

const piDoctrine = `Use the bus identity and addressing rules in the hcom bootstrap above.
For outbound collaboration, use ordinary hcom send with the correct target and intent; do not invent a parallel transport.
Never print, persist, or forward credential values.
A re-prompt can repeat content already present after a crash; recognize repeats and do not execute completed work twice blindly.
Respond when addressed or requested. Do not send speculative chatter or filler turns; silence is expected otherwise.`

func PiCredentialName(provider string) (string, error) {
	for _, candidate := range piProviderCredentials {
		if provider == candidate.provider {
			return candidate.key, nil
		}
	}
	return "", fmt.Errorf("Pi provider %q is unsupported; choose one of: anthropic, openai, xai", provider)
}

func ConfigurePiEnvironment(provider string) error {
	selected, err := ValidatePiCredential(provider)
	if err != nil {
		return err
	}
	for _, candidate := range piProviderCredentials {
		if candidate.key != selected {
			_ = os.Unsetenv(candidate.key)
		}
	}
	_ = os.Unsetenv("PI_CODING_AGENT_DIR")
	_ = os.Unsetenv("PI_CODING_AGENT_SESSION_DIR")
	_ = os.Setenv("PI_OFFLINE", "1")
	_ = os.Setenv("PI_TELEMETRY", "0")
	_ = os.Setenv("HCOM_NOTES", piDoctrine)
	return nil
}

func ValidatePiCredential(provider string) (string, error) {
	provider = strings.TrimSpace(provider)
	selected, err := PiCredentialName(provider)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(os.Getenv(selected)) == "" {
		return "", fmt.Errorf("Pi launch refused: selected provider credential %s is missing or empty; set %s and retry with --provider %s", selected, selected, provider)
	}
	return selected, nil
}

func ValidatePiExtraArgs(args []string, firstClassModel bool) error {
	for _, arg := range args {
		name := arg
		if before, _, ok := strings.Cut(arg, "="); ok {
			name = before
		}
		switch name {
		case "--model":
			if firstClassModel {
				return errors.New("--model conflicts with a model pin in --extra-arg; use the first-class --model flag or the passthrough form, not both")
			}
		case "--provider":
			return errors.New("Pi passthrough --provider conflicts with the required first-class --provider flag; use the first-class flag only")
		case "--api-key":
			return errors.New("Pi passthrough --api-key is refused because credentials are routed by named provider environment; set the selected provider credential and remove --api-key")
		case "--continue", "-c", "--resume", "-r", "--session", "--session-id", "--fork", "--session-dir", "--no-session":
			return fmt.Errorf("Pi passthrough %s attempts to re-point herder-owned session state; remove it and use herder resume/fork lifecycle commands", name)
		case "--offline":
			return errors.New("Pi passthrough --offline collides with herder's mandatory PI_OFFLINE=1 launch policy; remove the flag")
		}
		if strings.HasPrefix(strings.ToUpper(arg), "HOME=") || strings.HasPrefix(strings.ToUpper(arg), "PI_CODING_AGENT_DIR=") || strings.HasPrefix(strings.ToUpper(arg), "PI_CODING_AGENT_SESSION_DIR=") || strings.HasPrefix(strings.ToUpper(arg), "PI_OFFLINE=") || strings.HasPrefix(strings.ToUpper(arg), "PI_TELEMETRY=") {
			return fmt.Errorf("Pi passthrough %s attempts to override herder-owned home, state, offline, or telemetry policy; remove it", name)
		}
	}
	return nil
}

func ObservePiVendorVersion(now time.Time) (v2.VendorVersionObservation, error) {
	path, err := exec.LookPath("pi")
	if err != nil {
		return v2.VendorVersionObservation{}, errors.New("Pi launch refused: pi is not resolvable on PATH; install @earendil-works/pi-coding-agent and retry")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return v2.VendorVersionObservation{}, fmt.Errorf("Pi launch refused: resolve pi executable path: %w; reinstall @earendil-works/pi-coding-agent and retry", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return v2.VendorVersionObservation{}, fmt.Errorf("Pi launch refused: resolve pi executable symlinks: %w; reinstall @earendil-works/pi-coding-agent and retry", err)
	}
	for dir := filepath.Dir(resolved); ; dir = filepath.Dir(dir) {
		packagePath := filepath.Join(dir, "package.json")
		if data, readErr := os.ReadFile(packagePath); readErr == nil {
			var pkg struct {
				Name    string          `json:"name"`
				Version json.RawMessage `json:"version"`
			}
			if json.Unmarshal(data, &pkg) == nil && pkg.Name == piVendorPackage {
				version := "unknown"
				var parsed string
				if json.Unmarshal(pkg.Version, &parsed) == nil && strings.TrimSpace(parsed) != "" {
					version = parsed
				}
				return v2.VendorVersionObservation{Version: version, ObservedAt: now.UTC().Format("2006-01-02T15:04:05Z")}, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return v2.VendorVersionObservation{}, fmt.Errorf("Pi launch refused: executable %s is not owned by an %s package.json; install the vendor package and retry", resolved, piVendorPackage)
}

// PiExecutableDir returns the PATH directory containing the launchable pi
// entrypoint. It deliberately keeps the pre-symlink directory (for example,
// node_modules/.bin): ObservePiVendorVersion follows that symlink separately
// to prove package ownership. Login-shell launches must carry this directory
// forward after preflight instead of assuming shell startup preserves PATH.
func PiExecutableDir() (string, error) {
	path, err := exec.LookPath("pi")
	if err != nil {
		return "", errors.New("Pi launch refused: pi is not resolvable on PATH; install @earendil-works/pi-coding-agent and retry")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("Pi launch refused: resolve pi executable path: %w; reinstall @earendil-works/pi-coding-agent and retry", err)
	}
	return filepath.Dir(abs), nil
}

func RefreshPiVendorVersion(history *v2.VendorVersionHistory, observation v2.VendorVersionObservation) *v2.VendorVersionHistory {
	next := &v2.VendorVersionHistory{Current: observation}
	if history != nil && history.Current != (v2.VendorVersionObservation{}) {
		previous := history.Current
		next.Previous = &previous
	}
	return next
}
