package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLaunchBinUsesPath(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "herder")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	path, err := resolveLaunchBin("herder")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != script {
		t.Errorf("path = %q, want %q", path, script)
	}
}

func TestResolveLaunchBinRequiresPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := resolveLaunchBin("herder")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not on $PATH") || strings.Contains(err.Error(), "found in") {
		t.Errorf("error = %q, want PATH-only failure", err)
	}
}
