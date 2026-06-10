package store

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Backend is the narrow storage interface the store runs on. Names are
// store-root-relative, slash-separated paths. The v1 implementation is the
// local filesystem; a future remote backend only has to provide these five
// operations (the spec names read/write/list/atomic-swap; Delete is the
// minimal extension `bottle rm` needs so removed transcripts do not linger
// as orphaned files).
type Backend interface {
	// Read returns the full content of a file.
	Read(name string) ([]byte, error)
	// Write creates or replaces a file, creating parent directories.
	Write(name string, data []byte) error
	// List returns the relative slash-separated paths of every file under
	// dir, recursively, in lexical order; a missing dir lists as empty,
	// not as an error.
	List(dir string) ([]string, error)
	// AtomicSwap replaces a file atomically (temp file + rename), so
	// concurrent readers see either the old or the new content, never a
	// partial write.
	AtomicSwap(name string, data []byte) error
	// Delete removes a file or directory tree. Missing targets are not an
	// error.
	Delete(name string) error
}

// fsBackend is the local-filesystem Backend. Everything it creates follows
// the store's security posture: directories 0700, files 0600.
type fsBackend struct {
	root string
}

func (b *fsBackend) path(name string) string {
	return filepath.Join(b.root, filepath.FromSlash(name))
}

func (b *fsBackend) Read(name string) ([]byte, error) {
	return os.ReadFile(b.path(name))
}

func (b *fsBackend) Write(name string, data []byte) error {
	p := b.path(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func (b *fsBackend) List(dir string) ([]string, error) {
	root := b.path(dir)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}
	var names []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	return names, err
}

func (b *fsBackend) AtomicSwap(name string, data []byte) error {
	p := b.path(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".swap-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (b *fsBackend) Delete(name string) error {
	return os.RemoveAll(b.path(name))
}
