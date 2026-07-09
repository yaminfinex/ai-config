// Package ship implements the per-user shipper: session-file discovery
// (fsnotify hint + authoritative rescan), file identity, the cursor
// registry, and byte-range tailing against the frozen wire contract
// (docs/specs/sesh-wire.md). The shipper is dumb by decision: it never
// parses transcript JSONL and never keys identity on path or inode.
package ship

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"sesh/internal/wire"
)

// Identity is a shipped file's wire identity: session UUID from the filename
// convention plus (once the file reaches the fingerprint window) a content
// fingerprint. Path and inode are never identity.
type Identity struct {
	Tool      wire.Tool
	SessionID string
	FileUUID  string
}

// Key is the registry map key for this identity.
func (id Identity) Key() string {
	return string(id.Tool) + "/" + id.SessionID + "/" + id.FileUUID
}

// Roots are the two watched session roots. Either may itself be a symlink
// (resolved before walking); symlinks below a root are not followed.
type Roots struct {
	Claude string // ~/.claude/projects
	Codex  string // ~/.codex/sessions
}

// DefaultRoots derives the watched roots from the home directory.
func DefaultRoots(home string) Roots {
	return Roots{
		Claude: filepath.Join(home, ".claude", "projects"),
		Codex:  filepath.Join(home, ".codex", "sessions"),
	}
}

const uuidPattern = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

var (
	claudeName = regexp.MustCompile(`^(` + uuidPattern + `)\.jsonl$`)
	codexName  = regexp.MustCompile(`^rollout-.+-(` + uuidPattern + `)\.jsonl$`)
)

// Discovered is one matched session file at its current path. The path is
// where the identity lives right now; it is never part of the identity.
type Discovered struct {
	Identity Identity
	Path     string
}

// Discover walks both roots and returns every session file matching the
// discovery globs: <uuid>.jsonl under the Claude root, rollout-*-<uuid>.jsonl
// under the Codex root. Everything else is ignored. A missing root is not an
// error (the tool may not be installed on this box).
func Discover(roots Roots) ([]Discovered, error) {
	var out []Discovered
	walk := func(root string, tool wire.Tool, name *regexp.Regexp) error {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		return filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// A directory vanishing mid-walk is normal churn.
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.Type()&fs.ModeSymlink != 0 {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			m := name.FindStringSubmatch(d.Name())
			if m == nil {
				return nil
			}
			out = append(out, Discovered{
				Identity: Identity{Tool: tool, SessionID: m[1], FileUUID: m[1]},
				Path:     path,
			})
			return nil
		})
	}
	if err := walk(roots.Claude, wire.ToolClaude, claudeName); err != nil {
		return nil, err
	}
	if err := walk(roots.Codex, wire.ToolCodex, codexName); err != nil {
		return nil, err
	}
	return out, nil
}

// Fingerprint computes the wire fingerprint of the file at path: lowercase
// hex SHA-256 over bytes [0, FingerprintWindowBytes). ready is false while
// the file is below the window — identity is UUID-only until then.
func Fingerprint(path string) (fp string, ready bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	buf := make([]byte, wire.FingerprintWindowBytes)
	n, err := io.ReadFull(f, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		_ = n
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), true, nil
}
