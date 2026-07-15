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
	"strings"

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

// Roots are the watched session roots. Any may itself be a symlink
// (resolved before walking); symlinks below a root are not followed.
type Roots struct {
	Claude string // ~/.claude/projects
	Codex  string // ~/.codex/sessions
	Grok   string // ~/.grok/sessions
	Pi     string // ~/.pi/agent/sessions
}

// DefaultRoots derives the watched roots from the home directory.
func DefaultRoots(home string) Roots {
	return Roots{
		Claude: filepath.Join(home, ".claude", "projects"),
		Codex:  filepath.Join(home, ".codex", "sessions"),
		Grok:   filepath.Join(home, ".grok", "sessions"),
		Pi:     filepath.Join(home, ".pi", "agent", "sessions"),
	}
}

// All returns the roots in walk order.
func (r Roots) All() []string {
	return []string{r.Claude, r.Codex, r.Grok, r.Pi}
}

const uuidPattern = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

var (
	claudeName = regexp.MustCompile(`^(` + uuidPattern + `)\.jsonl$`)
	codexName  = regexp.MustCompile(`^rollout-.+-(` + uuidPattern + `)\.jsonl$`)
	piName     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}-[0-9]{3}Z_(` + uuidPattern + `)\.jsonl$`)
	uuidName   = regexp.MustCompile(`^` + uuidPattern + `$`)
)

// grokTranscriptName is the one file that ships from a grok session
// directory. Everything else in the directory is runtime state, not
// transcript, and everything above the sessions root is config/credentials —
// the exclusion is a security boundary, so admission is by exact shape, not
// by blocklist.
const grokTranscriptName = "chat_history.jsonl"

// Discovered is one matched session file at its current path. The path is
// where the identity lives right now; it is never part of the identity.
type Discovered struct {
	Identity Identity
	Path     string
}

// Discover walks the roots and returns every session file matching the
// discovery globs: <uuid>.jsonl under the Claude root, rollout-*-<uuid>.jsonl
// under the Codex root, <cwd-group>/<uuid>/chat_history.jsonl under the Grok
// root, and <cwd-key>/<timestamp>_<uuid>.jsonl under the Pi root. Everything
// else is ignored. A missing root is not an error (the tool may not be
// installed on this box).
func Discover(roots Roots) ([]Discovered, error) {
	var out []Discovered
	for _, w := range []struct {
		root  string
		tool  wire.Tool
		match func(rel string, d fs.DirEntry) (string, bool)
	}{
		{roots.Claude, wire.ToolClaude, nameMatch(claudeName)},
		{roots.Codex, wire.ToolCodex, nameMatch(codexName)},
		{roots.Grok, wire.ToolGrok, grokMatch},
		{roots.Pi, wire.ToolPi, piMatch},
	} {
		found, err := walkRoot(w.root, w.tool, w.match)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

// walkRoot walks one root with one admission matcher. It is the seam the
// exclusion-boundary test uses to prove its detector: the same walk with a
// deliberately widened matcher must trip the test's assertions.
func walkRoot(root string, tool wire.Tool, match func(rel string, d fs.DirEntry) (string, bool)) ([]Discovered, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Discovered
	err = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
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
		rel, err := filepath.Rel(resolved, path)
		if err != nil {
			return nil
		}
		uuid, ok := match(rel, d)
		if !ok {
			return nil
		}
		out = append(out, Discovered{
			Identity: Identity{Tool: tool, SessionID: uuid, FileUUID: uuid},
			Path:     path,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func nameMatch(name *regexp.Regexp) func(string, fs.DirEntry) (string, bool) {
	return func(_ string, d fs.DirEntry) (string, bool) {
		m := name.FindStringSubmatch(d.Name())
		if m == nil {
			return "", false
		}
		return m[1], true
	}
}

// grokMatch admits exactly <cwd-group>/<session-uuid>/chat_history.jsonl
// relative to the grok sessions root — the fixed transcript name, directly
// under a UUID-named session directory, exactly one cwd group deep. Depth and
// name are both load-bearing: session directories hold non-transcript runtime
// state (events, prompts, resources, rewind points, recap subdirectories) and
// the shape gate is what keeps any of it, or anything nested deeper, from
// ever shipping.
func grokMatch(rel string, _ fs.DirEntry) (string, bool) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 3 || parts[2] != grokTranscriptName || !uuidName.MatchString(parts[1]) {
		return "", false
	}
	return parts[1], true
}

// piMatch admits exactly <cwd-key>/<timestamp>_<session-uuid>.jsonl relative
// to Pi's default session root. The agent root's siblings are config,
// credentials, extensions, and runtime state; exact depth and filename
// admission keep that security boundary closed without a blocklist.
func piMatch(rel string, d fs.DirEntry) (string, bool) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || d.Type()&fs.ModeType != 0 {
		return "", false
	}
	m := piName.FindStringSubmatch(parts[1])
	if m == nil {
		return "", false
	}
	return m[1], true
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
