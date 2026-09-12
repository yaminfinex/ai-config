package fileapi

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestReadRawReturnsWholeFileThroughHardCap(t *testing.T) {
	root := t.TempDir()
	html := append([]byte("<!doctype html><main>"), bytes.Repeat([]byte("x"), 600*1024)...)
	html = append(html, []byte("<span id=tail></span></main>")...)
	if err := os.WriteFile(filepath.Join(root, "large.html"), html, 0o644); err != nil {
		t.Fatal(err)
	}
	exact := bytes.Repeat([]byte("z"), int(HardCap))
	if err := os.WriteFile(filepath.Join(root, "exact.html"), exact, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path string
		want []byte
	}{{"large.html", html}, {"exact.html", exact}} {
		got, info, err := ReadRaw(root, test.path)
		if err != nil {
			t.Fatalf("ReadRaw(%q): %v", test.path, err)
		}
		if !bytes.Equal(got, test.want) || info.Size() != int64(len(test.want)) {
			t.Fatalf("ReadRaw(%q) = %d bytes, info size %d; want %d byte-identical bytes", test.path, len(got), info.Size(), len(test.want))
		}
	}
}

func TestReadRawMirrorsReadRefusals(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "outside.html", "outside")
	if err := os.Symlink(filepath.Join(outside, "outside.html"), filepath.Join(root, "escape.html")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".git/config", "private")
	if err := os.Symlink(".git/config", filepath.Join(root, "git-alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	tooLarge, err := os.Create(filepath.Join(root, "too-large.html"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tooLarge.Truncate(HardCap + 1); err != nil {
		t.Fatal(err)
	}
	if err := tooLarge.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../escape.html", "escape.html", ".git/config", "git-alias", "directory", "too-large.html"} {
		if _, _, err := ReadRaw(root, path); !errors.Is(err, ErrRefused) {
			t.Errorf("ReadRaw(%q) error = %v, want ErrRefused", path, err)
		}
	}
}

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

func TestReadKeepsSplitRuneTextAndLargeNULFileClassificationsHonest(t *testing.T) {
	root := t.TempDir()
	text := strings.Repeat("a", int(SoftCap-1)) + strings.Repeat("—", 2000)
	writeFile(t, root, "split-rune.md", text)
	binaryBytes := make([]byte, SoftCap+100)
	for index := range binaryBytes {
		binaryBytes[index] = 'x'
	}
	binaryBytes[17] = 0
	if err := os.WriteFile(filepath.Join(root, "large-binary.bin"), binaryBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	invalidBytes := append([]byte(nil), binaryBytes...)
	invalidBytes[17] = 0xff
	if err := os.WriteFile(filepath.Join(root, "large-invalid.bin"), invalidBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	gotText, err := Read(root, "split-rune.md", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if gotText.Binary || gotText.Content == nil || gotText.Truncated == nil || !*gotText.Truncated || !utf8.ValidString(*gotText.Content) {
		t.Fatalf("split-rune text = %#v", gotText)
	}
	if len(*gotText.Content) != int(SoftCap-1) {
		t.Fatalf("split-rune content bytes = %d, want %d", len(*gotText.Content), SoftCap-1)
	}

	gotBinary, err := Read(root, "large-binary.bin", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if !gotBinary.Binary || gotBinary.Content != nil || gotBinary.Truncated != nil || gotBinary.Size != int64(len(binaryBytes)) {
		t.Fatalf("large binary = %#v", gotBinary)
	}

	gotInvalid, err := Read(root, "large-invalid.bin", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if !gotInvalid.Binary || gotInvalid.Content != nil || gotInvalid.Truncated != nil {
		t.Fatalf("large invalid UTF-8 binary = %#v", gotInvalid)
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
