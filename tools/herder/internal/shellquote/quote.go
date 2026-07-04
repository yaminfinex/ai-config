package shellquote

import "strings"

// Quote returns a shell word compatible with Bash printf %q for the characters
// herder passes through sh -lic export/exec strings.
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	if needsANSI(s) {
		return ansiQuote(s)
	}
	var b strings.Builder
	for _, r := range s {
		if isSafe(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('\\')
		b.WriteRune(r)
	}
	return b.String()
}

func needsANSI(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func ansiQuote(s string) string {
	var b strings.Builder
	b.WriteString("$'")
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\a':
			b.WriteString(`\a`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\v':
			b.WriteString(`\v`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

func isSafe(r rune) bool {
	if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '@', '%', '_', '+', '=', ':', '.', '/', '-', '#', '~':
		return true
	default:
		return r > 0x7f
	}
}
