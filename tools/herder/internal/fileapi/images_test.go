package fileapi

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var pngHead = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func imageFixtures() map[string][]byte {
	bmp := append([]byte("BM"), make([]byte, 16)...)
	bmp[14] = 40
	return map[string][]byte{
		"a.png":  pngHead,
		"a.jpg":  {0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F'},
		"a.gif":  []byte("GIF89a\x01\x00\x01\x00"),
		"a.webp": []byte("RIFF\x24\x00\x00\x00WEBPVP8 "),
		"a.avif": []byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1miaf"),
		"b.avif": []byte("\x00\x00\x00\x1cftypmif1\x00\x00\x00\x00mif1avifmiaf"),
		"a.bmp":  bmp,
		"a.ico":  {0, 0, 1, 0, 1, 0, 16, 16},
		"a.svg":  []byte("\xef\xbb\xbf<?xml version=\"1.0\"?>\n<!-- c -->\n<!DOCTYPE svg>\n<svg xmlns=\"http://www.w3.org/2000/svg\"/>"),
	}
}

func TestSniffImageAllowsOnlyAllowlistedBytes(t *testing.T) {
	want := map[string]string{
		"a.png": "image/png", "a.jpg": "image/jpeg", "a.gif": "image/gif", "a.webp": "image/webp",
		"a.avif": "image/avif", "b.avif": "image/avif", "a.bmp": "image/bmp", "a.ico": "image/x-icon", "a.svg": "image/svg+xml",
	}
	for name, head := range imageFixtures() {
		if got := SniffImage(name, head); got != want[name] {
			t.Errorf("SniffImage(%q) = %q, want %q", name, got, want[name])
		}
	}
	for name, head := range map[string][]byte{
		"x.png":        []byte("not a png\n"),
		"x.bmp":        []byte("BMW owners manual, chapter one\n"),
		"x.ico":        {0, 0, 1, 0, 0, 0},
		"x.webp":       []byte("RIFF\x24\x00\x00\x00WAVEfmt "),
		"x.avif":       []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00isommp41"),
		"x.svg":        []byte("<html><svg></svg></html>"),
		"x.svgz":       []byte("<svg/>"),
		"svg.txt":      []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>"),
		"x2.svg":       []byte("<svgfoo/>"),
		"x3.svg":       []byte("<svg\x00/>"),
		"unclosed.svg": []byte("<!-- <svg/>"),
		"empty.png":    {},
	} {
		if got := SniffImage(name, head); got != "" {
			t.Errorf("SniffImage(%q) = %q, want refusal", name, got)
		}
	}
}

func TestReadReportsImageMimeAndLiftsCapForImagesOnly(t *testing.T) {
	root := t.TempDir()
	for name, head := range imageFixtures() {
		if err := os.WriteFile(filepath.Join(root, name), head, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, "renamed.png", "plain text pretending\n")
	big := append(append([]byte{}, pngHead...), make([]byte, HardCap)...)
	if err := os.WriteFile(filepath.Join(root, "big.png"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	png, err := Read(root, "a.png", time.Now)
	if err != nil || !png.Binary || png.ImageMime != "image/png" || png.Content != nil {
		t.Fatalf("png = %#v, %v", png, err)
	}
	svg, err := Read(root, "a.svg", time.Now)
	if err != nil || svg.Binary || svg.ImageMime != "image/svg+xml" || svg.Content == nil {
		t.Fatalf("svg = %#v, %v", svg, err)
	}
	renamed, err := Read(root, "renamed.png", time.Now)
	if err != nil || renamed.Binary || renamed.ImageMime != "" || renamed.Content == nil {
		t.Fatalf("renamed = %#v, %v", renamed, err)
	}
	large, err := Read(root, "big.png", time.Now)
	if err != nil || !large.Binary || large.ImageMime != "image/png" || large.Size != int64(len(big)) {
		t.Fatalf("big png = %#v, %v", large, err)
	}
}

func TestOpenImageStreamsAllowedTypesAndRefusesEverythingElse(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for name, head := range imageFixtures() {
		if err := os.WriteFile(filepath.Join(root, name), head, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, "renamed.png", "plain text pretending\n")
	writeFile(t, root, "tiny.txt", "")
	if err := os.WriteFile(filepath.Join(outside, "outside.png"), pngHead, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.png"), filepath.Join(root, "escape.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "icon.png"), pngHead, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".git/icon.png", filepath.Join(root, "git-alias.png")); err != nil {
		t.Fatal(err)
	}
	atCap := append(append([]byte{}, pngHead...), make([]byte, ImageCap-int64(len(pngHead)))...)
	if err := os.WriteFile(filepath.Join(root, "at-cap.png"), atCap, 0o644); err != nil {
		t.Fatal(err)
	}
	overCap := append(atCap, 0)
	if err := os.WriteFile(filepath.Join(root, "over-cap.png"), overCap, 0o644); err != nil {
		t.Fatal(err)
	}

	for name, head := range imageFixtures() {
		file, info, mime, err := OpenImage(root, name)
		if err != nil {
			t.Fatalf("OpenImage(%q): %v", name, err)
		}
		got, _ := io.ReadAll(file)
		file.Close()
		if mime == "" || !bytes.Equal(got, head) || info.Size() != int64(len(head)) {
			t.Errorf("OpenImage(%q) = %q, %d bytes; want the whole file", name, mime, len(got))
		}
	}
	file, info, mime, err := OpenImage(root, "at-cap.png")
	if err != nil || mime != "image/png" || info.Size() != ImageCap {
		t.Fatalf("at-cap = %q %v", mime, err)
	}
	file.Close()
	for _, path := range []string{"renamed.png", "tiny.txt", "escape.png", ".git/icon.png", "git-alias.png", "../a.png", "over-cap.png"} {
		if _, _, _, err := OpenImage(root, path); !errors.Is(err, ErrRefused) {
			t.Errorf("OpenImage(%q) error = %v, want ErrRefused", path, err)
		}
	}
	if _, _, _, err := OpenImage(root, "over-cap.png"); err == nil || !strings.Contains(err.Error(), "25 MiB") {
		t.Errorf("over-cap error = %v, want 25 MiB", err)
	}
	if _, _, _, err := OpenImage(root, "missing.png"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing error = %v", err)
	}
	// Raw keeps the text cap even for an image above it.
	if _, _, err := ReadRaw(root, "at-cap.png"); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "4 MiB") {
		t.Errorf("ReadRaw(at-cap.png) error = %v, want 4 MiB refusal", err)
	}
}
