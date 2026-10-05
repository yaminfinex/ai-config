package fileapi

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	svgMime  = "image/svg+xml"
	sniffLen = 4096
)

// SniffImage names the image type of a file from its leading bytes, or
// returns "" when they match none of the allowed types. Raster types are
// decided by magic numbers alone. SVG is text, so it additionally needs the
// .svg extension and a root <svg> element; the extension is never enough.
func SniffImage(path string, head []byte) string {
	switch {
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(head, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return "image/gif"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "image/webp"
	case isAVIF(head):
		return "image/avif"
	case isBMP(head):
		return "image/bmp"
	case isICO(head):
		return "image/x-icon"
	case strings.EqualFold(filepath.Ext(path), ".svg") && isSVG(head):
		return svgMime
	}
	return ""
}

// isAVIF accepts an ISO-BMFF ftyp box whose major or compatible brand is avif
// or avis.
func isAVIF(head []byte) bool {
	if len(head) < 16 || string(head[4:8]) != "ftyp" {
		return false
	}
	size := int(binary.BigEndian.Uint32(head[:4]))
	if size < 16 || size > len(head) {
		size = len(head)
	}
	for offset := 8; offset+4 <= size; offset += 4 {
		if offset == 12 {
			continue // minor version
		}
		if brand := string(head[offset : offset+4]); brand == "avif" || brand == "avis" {
			return true
		}
	}
	return false
}

// isBMP needs more than "BM", which plain text can start with: the DIB header
// size must be one of the defined BITMAP*HEADER sizes.
func isBMP(head []byte) bool {
	if len(head) < 18 || string(head[:2]) != "BM" {
		return false
	}
	switch binary.LittleEndian.Uint32(head[14:18]) {
	case 12, 40, 52, 56, 64, 108, 124:
		return true
	}
	return false
}

// isICO accepts an ICONDIR with type 1 and at least one image.
func isICO(head []byte) bool {
	return len(head) >= 6 && bytes.HasPrefix(head, []byte{0, 0, 1, 0}) && binary.LittleEndian.Uint16(head[4:6]) > 0
}

// isSVG accepts UTF-8 text whose first element, after an optional BOM, XML
// declaration, comments, and doctype, is <svg.
func isSVG(head []byte) bool {
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	text := head
	for trimmed := 0; trimmed < utf8.UTFMax-1 && !utf8.Valid(text); trimmed++ {
		text = text[:len(text)-1]
	}
	if !utf8.Valid(text) {
		return false
	}
	rest := strings.TrimPrefix(string(text), "\ufeff")
	for {
		rest = strings.TrimLeft(rest, " \t\r\n")
		var end string
		switch {
		case strings.HasPrefix(rest, "<?"):
			end = "?>"
		case strings.HasPrefix(rest, "<!--"):
			end = "-->"
		case strings.HasPrefix(rest, "<!"):
			end = ">"
		default:
			if !strings.HasPrefix(rest, "<svg") || len(rest) == len("<svg") {
				return false
			}
			next := rest[len("<svg")]
			return next == ' ' || next == '\t' || next == '\r' || next == '\n' || next == '>' || next == '/'
		}
		index := strings.Index(rest, end)
		if index < 0 {
			return false
		}
		rest = rest[index+len(end):]
	}
}
