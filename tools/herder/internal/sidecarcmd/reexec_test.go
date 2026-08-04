package sidecarcmd

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pointerNameFor(root string) string {
	sum := sha256.Sum256([]byte(filepath.Join(root, "tools", "herder")))
	return ".herder-lastgood-" + hex.EncodeToString(sum[:])[:16]
}

func TestReexecTargetRefusesWhenNotACacheBinary(t *testing.T) {
	// The test binary lives in the go build cache, never in a herder cache
	// candidate — reexecTarget must refuse even with a valid pointer present.
	cache := t.TempDir()
	t.Setenv("AI_CONFIG_ROOT", "/repo")
	t.Setenv("XDG_CACHE_HOME", cache)
	dir := filepath.Join(cache, "herder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "herder-cafecafecafecafe")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pointerNameFor("/repo")), []byte(other+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, target := reexecTarget(); target != "" {
		t.Fatalf("reexecTarget = %q for a non-cache binary, want empty", target)
	}
}

func TestReexecTargetRefusesWithoutCheckoutRoot(t *testing.T) {
	t.Setenv("AI_CONFIG_ROOT", "")
	if _, target := reexecTarget(); target != "" {
		t.Fatalf("reexecTarget = %q without AI_CONFIG_ROOT, want empty", target)
	}
}

func TestReexecPointerResolutionWalksCandidatesAndChecksOwnership(t *testing.T) {
	root := "/checkout"
	pointer := pointerNameFor(root)
	xdg := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)
	t.Setenv("HOME", home)
	homeCache := filepath.Join(home, ".cache", "herder")
	if err := os.MkdirAll(homeCache, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(homeCache, "herder-1234567812345678")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// XDG candidate has no pointer; the HOME candidate's pointer must be found.
	if err := os.WriteFile(filepath.Join(homeCache, pointer), []byte(target+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidates := cacheCandidates()
	if len(candidates) < 2 || candidates[0] != filepath.Join(xdg, "herder") || candidates[1] != homeCache {
		t.Fatalf("cacheCandidates order = %v", candidates)
	}
	got := ""
	for _, dir := range candidates {
		b, err := os.ReadFile(filepath.Join(dir, pointer))
		if err != nil {
			continue
		}
		candidate := strings.TrimSpace(string(b))
		if candidate != "" && ownedExecutable(candidate) {
			got = candidate
			break
		}
	}
	if got != target {
		t.Fatalf("pointer walk resolved %q, want %q", got, target)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if ownedExecutable(target) {
		t.Fatal("non-executable pointer target must be refused")
	}
}

func TestMaybeReexecThrottlesChecks(t *testing.T) {
	t.Setenv("AI_CONFIG_ROOT", "")
	s := &sidecar{}
	s.maybeReexec()
	first := s.reexecCheckAt
	if first.IsZero() {
		t.Fatal("maybeReexec did not arm its recheck timer")
	}
	s.maybeReexec()
	if !s.reexecCheckAt.Equal(first) {
		t.Fatal("second call within the window must be a no-op")
	}
	s.reexecCheckAt = time.Now().Add(-time.Second)
	s.maybeReexec()
	if s.reexecCheckAt.Equal(first) {
		t.Fatal("expired window did not re-arm")
	}
}
