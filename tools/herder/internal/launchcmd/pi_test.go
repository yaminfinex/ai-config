package launchcmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestPiProviderContract(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai", "xai"} {
		if _, err := PiCredentialName(provider); err != nil {
			t.Errorf("PiCredentialName(%q): %v", provider, err)
		}
	}
	for _, provider := range []string{"", " ", "google", "ANTHROPIC"} {
		if _, err := PiCredentialName(provider); err == nil || !strings.Contains(err.Error(), "anthropic, openai, xai") {
			t.Errorf("PiCredentialName(%q) = %v, want supported-set refusal", provider, err)
		}
	}
}

func TestConfigurePiEnvironmentRoutesExactlyOneCredential(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("XAI_API_KEY", "xai-secret")
	t.Setenv("PI_CODING_AGENT_DIR", "/ambient/config-pin")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "/ambient/session-pin")
	t.Setenv("PI_OFFLINE", "ambient-offline")
	t.Setenv("PI_TELEMETRY", "ambient-telemetry")
	t.Setenv("HCOM_NOTES", "ambient-notes")

	if err := ConfigurePiEnvironment("openai"); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("OPENAI_API_KEY"); got != "openai-secret" {
		t.Fatalf("selected credential = %q, want inherited value", got)
	}
	for _, key := range []string{"ANTHROPIC_API_KEY", "XAI_API_KEY"} {
		if _, ok := os.LookupEnv(key); ok {
			t.Errorf("foreign credential %s remained in launch env", key)
		}
	}
	if os.Getenv("PI_OFFLINE") != "1" || os.Getenv("PI_TELEMETRY") != "0" {
		t.Fatalf("offline env = (%q,%q), want (1,0)", os.Getenv("PI_OFFLINE"), os.Getenv("PI_TELEMETRY"))
	}
	for _, want := range []string{"outbound", "credential", "repeat", "silence", "hcom send"} {
		if !strings.Contains(strings.ToLower(os.Getenv("HCOM_NOTES")), want) {
			t.Errorf("HCOM_NOTES missing %q", want)
		}
	}
	if got, ok := os.LookupEnv("PI_CODING_AGENT_DIR"); ok {
		t.Fatalf("PI_CODING_AGENT_DIR pin = %q, want absent", got)
	}
	if got, ok := os.LookupEnv("PI_CODING_AGENT_SESSION_DIR"); ok {
		t.Fatalf("PI_CODING_AGENT_SESSION_DIR pin = %q, want absent", got)
	}
}

func TestConfigurePiEnvironmentRefusesMissingCredential(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	err := ConfigurePiEnvironment("openai")
	if err == nil {
		t.Fatal("missing selected credential accepted")
	}
	for _, want := range []string{"OPENAI_API_KEY", "set", "--provider openai"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestValidatePiExtraArgsRefusesOwnedSurfaces(t *testing.T) {
	cases := [][]string{
		{"--provider", "openai"}, {"--provider=openai"},
		{"--api-key", "secret"}, {"--api-key=secret"},
		{"--session", "sid"}, {"--session=sid"}, {"--session-id", "sid"},
		{"--fork", "sid"}, {"--resume"}, {"--continue"}, {"-r"}, {"-c"},
		{"--session-dir", "/tmp/s"}, {"--no-session"}, {"--offline"},
	}
	for _, args := range cases {
		if err := ValidatePiExtraArgs(args, false); err == nil {
			t.Errorf("ValidatePiExtraArgs(%v) accepted owned surface", args)
		}
	}
	for _, args := range [][]string{{"--thinking", "high"}, {"--no-tools"}, {"--future-vendor-flag", "value"}} {
		if err := ValidatePiExtraArgs(args, false); err != nil {
			t.Errorf("ValidatePiExtraArgs(%v) rejected unowned surface: %v", args, err)
		}
	}
	if err := ValidatePiExtraArgs([]string{"--model", "x"}, true); err == nil || !strings.Contains(err.Error(), "--model conflicts") {
		t.Fatalf("first-class model collision = %v", err)
	}
	if err := ValidatePiExtraArgs([]string{"--model", "x"}, false); err != nil {
		t.Fatalf("passthrough model without first-class pin rejected: %v", err)
	}
}

func TestPiLaunchRunCallSiteRefusesOwnedPassthrough(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "selected")
	t.Setenv("PATH", t.TempDir()) // a removed call-site guard fails safely at Pi observation, never execs hcom
	var stderr strings.Builder
	if code := Run([]string{"pi", "--provider", "openai", "--api-key", "stand-in"}, io.Discard, &stderr); code == 0 {
		t.Fatal("Pi launch accepted an owned credential passthrough")
	}
	for _, want := range []string{"--api-key", "refused", "environment"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("launch refusal missing %q: %s", want, stderr.String())
		}
	}
}

func TestObservePiVendorVersionResolvesSymlinkWithoutExecution(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "lib", "node_modules", "@earendil-works", "pi-coding-agent")
	if err := os.MkdirAll(filepath.Join(pkg, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(pkg, "dist", "cli.js")
	if err := os.WriteFile(entry, []byte("process.exit(99)\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@earendil-works/pi-coding-agent","version":"0.80.6"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(entry, filepath.Join(bin, "pi")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	obs, err := ObservePiVendorVersion(time.Date(2026, 7, 15, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if obs.Version != "0.80.6" || obs.ObservedAt != "2026-07-15T01:02:03Z" {
		t.Fatalf("observation = %+v", obs)
	}
	gotBin, err := PiExecutableDir()
	if err != nil {
		t.Fatal(err)
	}
	if gotBin != bin {
		t.Fatalf("PiExecutableDir() = %q, want pre-symlink launch directory %q", gotBin, bin)
	}
}

func TestObservePiVendorVersionRecordsUnknownForUnparseableVersion(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(pkg, "pi")
	if err := os.WriteFile(entry, []byte("not executed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@earendil-works/pi-coding-agent","version":17}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pkg)
	obs, err := ObservePiVendorVersion(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if obs.Version != "unknown" {
		t.Fatalf("version = %q, want unknown", obs.Version)
	}
}

func TestObservePiVendorVersionRefusesUnresolvableInstallWithRemedy(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := ObservePiVendorVersion(time.Now())
	if err == nil {
		t.Fatal("unresolvable Pi install accepted")
	}
	for _, want := range []string{"pi is not resolvable", "install", "@earendil-works/pi-coding-agent"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestRefreshPiVendorVersionKeepsCurrentAndPrevious(t *testing.T) {
	first := v2.VendorVersionObservation{Version: "0.80.5", ObservedAt: "2026-07-15T00:00:00Z"}
	second := v2.VendorVersionObservation{Version: "0.80.6", ObservedAt: "2026-07-15T01:00:00Z"}
	history := RefreshPiVendorVersion(nil, first)
	history = RefreshPiVendorVersion(history, second)
	if history == nil || history.Current != second || history.Previous == nil || *history.Previous != first {
		t.Fatalf("history = %+v, want current second / previous first", history)
	}
}

func TestPiLaunchBoundaryDoesNotGateVersionHashOrDrift(t *testing.T) {
	for _, tc := range []struct {
		name        string
		versionJSON string
		wantVersion string
	}{
		{name: "old-version", versionJSON: `"0.0.1"`, wantVersion: "0.0.1"},
		{name: "future-version", versionJSON: `"999.999.999"`, wantVersion: "999.999.999"},
		{name: "unparseable-version", versionJSON: `17`, wantVersion: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			pkg := filepath.Join(root, "node_modules", "@earendil-works", "pi-coding-agent")
			entry := filepath.Join(pkg, "dist", "cli.js")
			if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(entry, []byte("arbitrary executable contents are observed, never hashed or run\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			manifest := `{"name":"@earendil-works/pi-coding-agent","version":` + tc.versionJSON + `}`
			if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(root, "bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(entry, filepath.Join(bin, "pi")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin) // deliberately no hcom: a policy-free launch reaches that boundary
			t.Setenv("OPENAI_API_KEY", "selected")
			t.Setenv("ANTHROPIC_API_KEY", "foreign")
			t.Setenv("XAI_API_KEY", "foreign")
			t.Setenv("PI_OFFLINE", "ambient")
			t.Setenv("PI_TELEMETRY", "ambient")
			t.Setenv("HCOM_NOTES", "ambient")

			observation, err := ObservePiVendorVersion(time.Now())
			if err != nil || observation.Version != tc.wantVersion {
				t.Fatalf("observation = (%+v, %v), want version %q without refusal", observation, err, tc.wantVersion)
			}
			payload, err := json.Marshal(observation)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"hash", "digest", "checksum"} {
				if strings.Contains(strings.ToLower(string(payload)), forbidden) {
					t.Fatalf("vendor observation grew a forbidden integrity gate field: %s", payload)
				}
			}
			if typ := reflect.TypeOf(observation); typ.NumField() != 2 {
				t.Fatalf("vendor observation has %d fields, want version + observed_at only (no hash contract)", typ.NumField())
			}

			var stderr strings.Builder
			if code := Run([]string{"pi", "--provider", "openai"}, io.Discard, &stderr); code == 0 {
				t.Fatal("launch unexpectedly succeeded without hcom")
			}
			if !strings.Contains(stderr.String(), "hcom not on PATH") {
				t.Fatalf("launch terminated before hcom resolution, indicating a version/hash/drift gate: %s", stderr.String())
			}
		})
	}
}
