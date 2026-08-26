package servecmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceWatchSnapshotTracksWrapperInputsOnly(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module test\n")
	write("cmd/herder/main.go", "package main\n")
	write("internal/pkg/code.go", "package pkg\n")
	write("internal/webui/dist/app.js", "one")
	write("internal/ignored.txt", "ignored")

	target := sourceWatchTarget{root: root}
	initial, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	write("internal/ignored.txt", "changed")
	ignored, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if ignored != initial {
		t.Fatal("non-wrapper input changed the source snapshot")
	}
	write("internal/webui/dist/app.js", "two")
	asset, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if asset == initial {
		t.Fatal("embedded distribution change did not change source snapshot")
	}
}

func TestExecutableWatchSnapshotFollowsSymlinkReplacement(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	link := filepath.Join(root, "herder")
	if err := os.WriteFile(first, []byte("one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("two-two"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	target := executableWatchTarget{path: link}
	before, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	after, err := target.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("symlink target replacement did not change executable snapshot")
	}
}
