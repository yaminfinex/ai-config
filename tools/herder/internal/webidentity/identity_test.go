package webidentity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSenderDerivesStableWebOriginFromTailscaleWhois(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	stub := filepath.Join(dir, "tailscale")
	script := `#!/usr/bin/env bash
printf '%s\n' "$*" >"$WHOIS_ARGS"
printf '%s\n' '{"UserProfile":{"LoginName":"Alice+Ops@Example.COM"}}'
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("WHOIS_ARGS", log)
	sender, err := Sender(context.Background(), "100.64.0.8:44321")
	if err != nil || sender != "web-alice-ops-example-com" {
		t.Fatalf("sender=%q err=%v", sender, err)
	}
	args, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "whois --json 100.64.0.8\n" {
		t.Fatalf("tailscale args = %q", args)
	}
}

func TestSenderClassifiesWhoisFailureAndSemanticIdentityRefusals(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "tailscale")
	if err := os.WriteFile(stub, []byte("#!/usr/bin/env bash\nprintf 'no tailnet identity for loopback\\n' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	if _, err := Sender(context.Background(), "127.0.0.1:4400"); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "no tailnet identity") {
		t.Fatalf("loopback error = %v", err)
	}
	if err := os.WriteFile(stub, []byte("#!/usr/bin/env bash\nprintf '%s\\n' '{\"UserProfile\":{}}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Sender(context.Background(), "100.64.0.9:1234"); err == nil || errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "no user login") {
		t.Fatalf("missing login error = %v", err)
	}
	if err := os.WriteFile(stub, []byte("#!/usr/bin/env bash\nprintf '%s\\n' '{\"UserProfile\":{\"LoginName\":\"bigboss@example.com\"}}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Sender(context.Background(), "100.64.0.9:1234"); err == nil || errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved error = %v", err)
	}
}
