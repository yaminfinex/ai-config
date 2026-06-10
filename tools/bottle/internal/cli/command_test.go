package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resolveLaunchBin falls back to the herder skill scripts dir when the binary
// is not on $PATH — the live failure mode where an installed bottle could not
// exec herder-spawn (not on $PATH) even though the deployed skill had it.
func TestResolveLaunchBinFallsBackToScriptsDir(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "herder-spawn")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A name that won't be on $PATH, resolved via the fallback dir.
	path, note, err := resolveLaunchBin("herder-spawn", []string{filepath.Join(dir, "absent"), dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != script {
		t.Errorf("path = %q, want %q", path, script)
	}
	if !strings.Contains(note, "not on $PATH") || !strings.Contains(note, script) {
		t.Errorf("note should name the chosen fallback path: %q", note)
	}
}

// A non-executable file in a fallback dir must be skipped, not chosen.
func TestResolveLaunchBinSkipsNonExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "herder-spawn"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveLaunchBin("herder-spawn", []string{dir}); err == nil {
		t.Fatal("expected error for non-executable fallback, got nil")
	}
}

// With no fallback dirs and nothing on $PATH, the error stays a plain
// not-on-$PATH message (no empty "found in" list).
func TestResolveLaunchBinNoFallbackDirs(t *testing.T) {
	_, _, err := resolveLaunchBin("definitely-not-a-real-binary-xyz", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not on $PATH") || strings.Contains(err.Error(), "found in") {
		t.Errorf("error = %q, want plain not-on-$PATH", err)
	}
}
