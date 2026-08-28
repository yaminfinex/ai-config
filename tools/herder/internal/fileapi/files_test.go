package fileapi

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadTextReportsAsNowShapeAndSoftTruncation(t *testing.T) {
	root := t.TempDir()
	fetched := time.Date(2026, 8, 28, 2, 0, 0, 731, time.UTC)
	writeFile(t, root, "small.md", "hello\n")
	writeFile(t, root, "large.md", strings.Repeat("x", int(SoftCap+17)))

	small, err := Read(root, "small.md", func() time.Time { return fetched })
	if err != nil {
		t.Fatal(err)
	}
	if small.Root != root || small.Path != "small.md" || small.Content == nil || *small.Content != "hello\n" || small.Binary || small.Size != 6 || small.Truncated == nil || *small.Truncated || !small.FetchedAt.Equal(fetched) {
		t.Fatalf("small = %#v", small)
	}
	large, err := Read(root, "large.md", func() time.Time { return fetched })
	if err != nil {
		t.Fatal(err)
	}
	if large.Content == nil || len(*large.Content) != int(SoftCap) || large.Truncated == nil || !*large.Truncated || large.Size != SoftCap+17 {
		t.Fatalf("large = %#v", large)
	}
}

func TestReadBinaryOmitsContentTruncationAndRefusesHardCap(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "binary.bin"), []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	binary, err := Read(root, "binary.bin", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if !binary.Binary || binary.Content != nil || binary.Truncated != nil || binary.Size != 4 {
		t.Fatalf("binary = %#v", binary)
	}
	tooLarge := filepath.Join(root, "too-large.txt")
	file, err := os.Create(tooLarge)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(HardCap + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root, "too-large.txt", time.Now); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("hard-cap error = %v", err)
	}
}

func TestTreeIsOneLevelStableAndHidesOnlyGitInternals(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".hidden", "hidden\n")
	writeFile(t, root, ".git/config", "private\n")
	writeFile(t, root, "dir/nested.md", "nested\n")
	writeFile(t, root, "visible.md", "visible\n")
	if err := os.Symlink("visible.md", filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".git/config", filepath.Join(root, "git-alias")); err != nil {
		t.Fatal(err)
	}

	tree, err := Tree(root, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Name: ".hidden", Kind: "file", Size: int64Pointer(7)},
		{Name: "dir", Kind: "directory"},
		{Name: "git-alias", Kind: "symlink"},
		{Name: "link.md", Kind: "symlink"},
		{Name: "visible.md", Kind: "file", Size: int64Pointer(8)},
	}
	if len(tree.Entries) != len(want) {
		t.Fatalf("entries = %#v", tree.Entries)
	}
	for index := range want {
		got := tree.Entries[index]
		if got.Name != want[index].Name || got.Kind != want[index].Kind || !equalSize(got.Size, want[index].Size) {
			t.Fatalf("entry[%d] = %#v want %#v", index, got, want[index])
		}
	}
	if _, err := Read(root, ".git/config", time.Now); !errors.Is(err, ErrRefused) {
		t.Fatalf(".git error = %v", err)
	}
	if _, err := Read(root, "git-alias", time.Now); !errors.Is(err, ErrRefused) {
		t.Fatalf(".git alias error = %v", err)
	}
}

func TestReadAndTreeRefuseSymlinkEscapeQuotingRequestedAndResolvedPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.md")
	writeFile(t, outside, "outside.md", "outside\n")
	if err := os.Symlink(outsideFile, filepath.Join(root, "escape.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape-dir")); err != nil {
		t.Fatal(err)
	}
	for _, run := range []func() error{
		func() error { _, err := Read(root, "escape.md", time.Now); return err },
		func() error { _, err := Tree(root, "escape-dir"); return err },
	} {
		err := run()
		if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), outside) {
			t.Fatalf("escape error = %v", err)
		}
	}
}

func writeFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func int64Pointer(value int64) *int64 { return &value }

func equalSize(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
