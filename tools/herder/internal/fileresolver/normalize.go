package fileresolver

import (
	"strconv"
	"strings"
	"unicode"
)

// NormalizedQuery separates a path-like mention from its optional one-based
// line suffix. Line is zero when the mention did not carry a valid suffix.
type NormalizedQuery struct {
	Path string
	Line int
}

// NormalizeQuery removes prose fencing and fused punctuation while preserving
// literal spaces and Unicode characters within the filename.
func NormalizeQuery(input string) NormalizedQuery {
	path := strings.TrimSpace(input)
	path = strings.TrimLeftFunc(path, isLeadingFence)
	path = strings.TrimRightFunc(path, isTrailingFence)

	line := 0
	if colon := strings.LastIndexByte(path, ':'); colon >= 0 && colon+1 < len(path) {
		suffix := path[colon+1:]
		if allDigits(suffix) {
			if parsed, err := strconv.Atoi(suffix); err == nil && parsed > 0 {
				path = path[:colon]
				line = parsed
			}
		}
	}
	path = strings.TrimRightFunc(path, isTrailingFence)
	return NormalizedQuery{Path: path, Line: line}
}

func isLeadingFence(r rune) bool {
	return r == '`' || r == '"' || r == '\'' || r == '“' || r == '‘'
}

func isTrailingFence(r rune) bool {
	switch r {
	case '`', '"', '\'', '”', '’', ',', ';', ':', '.', '!', '?', ')', ']', '}', '>':
		return true
	default:
		return unicode.IsSpace(r)
	}
}

func allDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}
