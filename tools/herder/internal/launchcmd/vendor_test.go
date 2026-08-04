package launchcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExec(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// The resolver must skip a herder shim (marker in head), a mise shims-dir
// dispatcher, and an npm copy inside a mise-managed install, then land on the
// vendor binary — regardless of PATH order putting the imposters first.
func TestResolveVendorToolSkipsShimsAndMise(t *testing.T) {
	root := t.TempDir()

	shimDir := filepath.Join(root, "checkout", "tools", "herder", "shims")
	miseShims := filepath.Join(root, "home", ".local", "share", "mise", "shims")
	nodeBin := filepath.Join(root, "home", ".local", "share", "mise", "installs", "node", "25.9.0", "bin")
	vendorDir := filepath.Join(root, "home", ".local", "bin")
	for _, d := range []string{shimDir, miseShims, nodeBin, vendorDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeExec(t, shimDir, "claude", "#!/bin/sh\n# herder-path-shim: claude\nexit 0\n")
	writeExec(t, miseShims, "claude", "#!/bin/sh\nexec mise x -- claude\n")
	writeExec(t, nodeBin, "claude", "#!/bin/sh\necho npm copy\n")
	vendor := writeExec(t, vendorDir, "claude", "#!/bin/sh\necho vendor\n")

	t.Setenv("PATH", strings.Join([]string{shimDir, miseShims, nodeBin, vendorDir}, string(os.PathListSeparator)))

	got, err := resolveVendorTool("claude")
	if err != nil {
		t.Fatalf("resolveVendorTool: %v", err)
	}
	if got != vendor {
		t.Fatalf("resolved %q, want vendor %q", got, vendor)
	}
}

func TestResolveVendorToolSkipsMiseSymlinkDispatcher(t *testing.T) {
	root := t.TempDir()
	// A shims-style dir OUTSIDE any /mise/ path whose entry symlinks to the
	// mise binary — only the symlink-target rule can reject it.
	linkDir := filepath.Join(root, "othershims")
	binDir := filepath.Join(root, "bin")
	for _, d := range []string{linkDir, binDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	miseBin := writeExec(t, root, "mise", "#!/bin/sh\nexit 0\n")
	if err := os.Symlink(miseBin, filepath.Join(linkDir, "claude")); err != nil {
		t.Fatal(err)
	}
	vendor := writeExec(t, binDir, "claude", "#!/bin/sh\necho vendor\n")

	t.Setenv("PATH", strings.Join([]string{linkDir, binDir}, string(os.PathListSeparator)))

	got, err := resolveVendorTool("claude")
	if err != nil {
		t.Fatalf("resolveVendorTool: %v", err)
	}
	if got != vendor {
		t.Fatalf("resolved %q, want vendor %q", got, vendor)
	}
}

func TestResolveVendorToolErrorsWhenOnlyImpostersExist(t *testing.T) {
	root := t.TempDir()
	miseShims := filepath.Join(root, "mise", "shims")
	if err := os.MkdirAll(miseShims, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, miseShims, "claude", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", miseShims)

	if _, err := resolveVendorTool("claude"); err == nil {
		t.Fatal("expected error when only mise-owned candidates exist")
	}
}

func TestVendorPinDirCreatesAndRetargets(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	vendorA := writeExec(t, root, "claude-a", "#!/bin/sh\n")
	vendorB := writeExec(t, root, "claude-b", "#!/bin/sh\n")

	dir, err := vendorPinDir("claude", vendorA)
	if err != nil {
		t.Fatalf("vendorPinDir: %v", err)
	}
	link := filepath.Join(dir, "claude")
	if got, _ := os.Readlink(link); got != vendorA {
		t.Fatalf("link -> %q, want %q", got, vendorA)
	}

	// Retarget on change, stable when unchanged.
	if _, err := vendorPinDir("claude", vendorB); err != nil {
		t.Fatalf("vendorPinDir retarget: %v", err)
	}
	if got, _ := os.Readlink(link); got != vendorB {
		t.Fatalf("link -> %q, want retarget %q", got, vendorB)
	}
}

func TestPrependPathEnv(t *testing.T) {
	env := []string{"HOME=/h", "PATH=/usr/bin:/bin", "TERM=x"}
	out := prependPathEnv(env, "/pin")
	want := "PATH=/pin" + string(os.PathListSeparator) + "/usr/bin:/bin"
	found := false
	for _, item := range out {
		if item == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("prepended PATH missing; got %v", out)
	}
	// No PATH in input → PATH added.
	out = prependPathEnv([]string{"HOME=/h"}, "/pin")
	if out[len(out)-1] != "PATH=/pin" {
		t.Fatalf("expected fresh PATH entry, got %v", out)
	}
}
