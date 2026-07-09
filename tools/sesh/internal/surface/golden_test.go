package surface_test

// Golden snapshots: every fixture session's render, byte-for-byte, plus a
// well-formedness check over our own HTML output (plan U7: "template render
// of every fixture session produces valid HTML (golden snapshots)").
// Regenerate with: go test ./internal/surface -run Golden -update

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s unreadable (regenerate with -update): %v", name, err)
	}
	if !bytes.Equal(want, []byte(got)) {
		i := 0
		for i < len(want) && i < len(got) && want[i] == got[i] {
			i++
		}
		lo := max(0, i-120)
		t.Errorf("render diverges from golden %s at byte %d:\n golden: …%.240q\n got:    …%.240q",
			name, i, string(want[lo:min(len(want), i+120)]), got[lo:min(len(got), i+120)])
	}
}

func TestGoldenSnapshots(t *testing.T) {
	srv := newServer(t, corpusStore(t))
	pages := map[string]string{
		"recency.html":                     "/",
		"recency-fragment.html":            "/fragments/recency",
		"transcript-claude-normal.html":    "/s/claude/" + uuidNormal,
		"transcript-resume-pair.html":      "/s/claude/" + uuidResumeOrig,
		"transcript-interleaved.html":      "/s/claude/" + uuidInterleave,
		"transcript-codex.html":            "/s/codex/" + uuidCodexMeta,
		"fallback-quarantined-raw.html":    "/s/claude/" + uuidPartial,
		"raw-claude-normal.html":           "/s/claude/" + uuidNormal + "/raw",
	}
	for name, path := range pages {
		t.Run(name, func(t *testing.T) {
			body := mustGet200(t, srv, path)
			assertWellFormedHTML(t, body)
			checkGolden(t, name, body)
		})
	}
}

// --- minimal strict well-formedness checker for our own output ---
//
// html/template guarantees escaping; this guards structure: every '<' opens
// a real tag, attribute values are quoted, non-void tags balance. It is a
// check on HTML we author, not a general parser.

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"source": true, "track": true, "wbr": true,
}

func assertWellFormedHTML(t *testing.T, page string) {
	t.Helper()
	var stack []string
	i := 0
	for {
		lt := strings.IndexByte(page[i:], '<')
		if lt < 0 {
			break
		}
		pos := i + lt
		rest := page[pos:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest, "-->")
			if end < 0 {
				t.Fatalf("unterminated comment at byte %d", pos)
			}
			i = pos + end + 3
		case strings.HasPrefix(rest, "<!"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				t.Fatalf("unterminated doctype at byte %d", pos)
			}
			i = pos + end + 1
		case strings.HasPrefix(rest, "</"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				t.Fatalf("unterminated closing tag at byte %d", pos)
			}
			name := strings.ToLower(strings.TrimSpace(rest[2:end]))
			if len(stack) == 0 {
				t.Fatalf("closing </%s> at byte %d with nothing open", name, pos)
			}
			if top := stack[len(stack)-1]; top != name {
				t.Fatalf("closing </%s> at byte %d, but <%s> is open", name, pos, top)
			}
			stack = stack[:len(stack)-1]
			i = pos + end + 1
		default:
			name, next, selfClose := scanOpenTag(t, page, pos)
			if name == "script" || name == "style" {
				closer := "</" + name + ">"
				end := strings.Index(page[next:], closer)
				if end < 0 {
					t.Fatalf("unterminated <%s> at byte %d", name, pos)
				}
				i = next + end + len(closer)
				continue
			}
			if !selfClose && !voidElements[name] {
				stack = append(stack, name)
			}
			i = next
		}
	}
	if len(stack) != 0 {
		t.Fatalf("unclosed tags at end of document: %v", stack)
	}
}

// scanOpenTag parses an opening tag starting at pos ('<'), quote-aware so a
// '>' inside an attribute value does not end the tag. Returns the tag name,
// the offset just past '>', and whether the tag self-closed.
func scanOpenTag(t *testing.T, page string, pos int) (name string, next int, selfClose bool) {
	t.Helper()
	j := pos + 1
	for j < len(page) && (isASCIILetter(page[j]) || isASCIIDigit(page[j])) {
		j++
	}
	if j == pos+1 {
		t.Fatalf("stray '<' at byte %d: %.40q", pos, page[pos:])
	}
	name = strings.ToLower(page[pos+1 : j])
	for j < len(page) {
		switch page[j] {
		case '"', '\'':
			quote := page[j]
			end := strings.IndexByte(page[j+1:], quote)
			if end < 0 {
				t.Fatalf("unterminated attribute quote in <%s> at byte %d", name, pos)
			}
			j += end + 2
		case '=':
			// Attribute values must be quoted in our output.
			if j+1 >= len(page) || (page[j+1] != '"' && page[j+1] != '\'') {
				t.Fatalf("unquoted attribute value in <%s> at byte %d", name, pos)
			}
			j++
		case '>':
			return name, j + 1, j > pos && page[j-1] == '/'
		default:
			j++
		}
	}
	t.Fatalf("unterminated tag <%s> at byte %d", name, pos)
	return "", 0, false
}

func isASCIILetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }
func isASCIIDigit(b byte) bool  { return b >= '0' && b <= '9' }
