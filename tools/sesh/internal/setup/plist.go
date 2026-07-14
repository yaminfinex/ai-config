package setup

import (
	"fmt"
	"strings"
)

// The macOS node-local config surface is a single launchd plist that carries
// BOTH the pinned executable path (ProgramArguments[0]) and SESH_STORE_URL.
// A rewrite is surgical: it replaces exactly those two <string> values,
// inserts the SoftResourceLimits block when an older render lacks it, and
// leaves every other byte of the plist untouched — the ProgramArguments
// array's structure and its "ship" argument are never disturbed. The
// provenance digest rides as a trailing XML comment after </plist> (valid
// XML trailing Misc; plistlib and CoreFoundation skip comments).

// RenderPlist computes the launchd plist for exePath and storeURL, stamped
// with the provenance digest. existing is the current plist content (nil
// when absent) whose unknown keys are preserved verbatim.
func RenderPlist(existing []byte, exePath, storeURL, home string) ([]byte, error) {
	body, _ := plistCommentStyle.split(existing)
	if len(body) == 0 {
		fresh := strings.NewReplacer(
			"@SESH_BIN@", xmlEscape(exePath),
			"@SESH_STORE_URL@", xmlEscape(storeURL),
			"@HOME@", xmlEscape(home),
		).Replace(plistTemplate)
		return plistCommentStyle.stamp([]byte(fresh)), nil
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if err := replacePlistString(lines, "<key>"+storeURLKey+"</key>", storeURL); err != nil {
		return nil, err
	}
	if err := replacePlistString(lines, "<key>ProgramArguments</key>", exePath); err != nil {
		return nil, err
	}
	lines, err := insertPlistSoftLimits(lines)
	if err != nil {
		return nil, err
	}
	return plistCommentStyle.stamp([]byte(strings.Join(lines, "\n") + "\n")), nil
}

// insertPlistSoftLimits adds the SoftResourceLimits block to a rewritten
// plist that predates it, before the top-level </dict>. launchd's default
// 256-fd soft limit starves kqueue-based fsnotify over a large session corpus
// (one fd per watched directory plus the shipper's own files; 4k+ fds
// observed on a heavy node running foreground), so setup raises it — on
// re-runs too, not just fresh renders, or already-onboarded Macs would never
// gain the limit.
func insertPlistSoftLimits(lines []string) ([]string, error) {
	for _, l := range lines {
		if strings.Contains(l, "<key>SoftResourceLimits</key>") {
			return lines, nil
		}
	}
	last := -1
	for i, l := range lines {
		if strings.Contains(l, "</dict>") {
			last = i
		}
	}
	if last < 0 {
		return nil, fmt.Errorf("plist: no closing </dict> to insert SoftResourceLimits before")
	}
	block := []string{
		"    <key>SoftResourceLimits</key>",
		"    <dict>",
		"        <key>NumberOfFiles</key>",
		"        <integer>8192</integer>",
		"    </dict>",
	}
	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:last]...)
	out = append(out, block...)
	out = append(out, lines[last:]...)
	return out, nil
}

// replacePlistString finds the marker line and replaces the content of the
// first <string> element on a following line, preserving indentation.
func replacePlistString(lines []string, marker, newValue string) error {
	for i, line := range lines {
		if !strings.Contains(line, marker) {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			open := strings.Index(lines[j], "<string>")
			if open < 0 {
				continue
			}
			closeIdx := strings.Index(lines[j], "</string>")
			if closeIdx < open {
				return fmt.Errorf("plist: malformed <string> element after %s", marker)
			}
			lines[j] = lines[j][:open+len("<string>")] + xmlEscape(newValue) + lines[j][closeIdx:]
			return nil
		}
		return fmt.Errorf("plist: no <string> value found after %s", marker)
	}
	return fmt.Errorf("plist: %s not found; the file is too far from setup's shape to rewrite (edit it directly or remove it and re-run)", marker)
}

// PlistStoreURL extracts SESH_STORE_URL from plist content.
func PlistStoreURL(content []byte) (string, bool) {
	return plistStringAfter(content, "<key>"+storeURLKey+"</key>")
}

// PlistExecPath extracts ProgramArguments[0] from plist content.
func PlistExecPath(content []byte) (string, bool) {
	return plistStringAfter(content, "<key>ProgramArguments</key>")
}

func plistStringAfter(content []byte, marker string) (string, bool) {
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		if !strings.Contains(line, marker) {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			open := strings.Index(lines[j], "<string>")
			if open < 0 {
				continue
			}
			closeIdx := strings.Index(lines[j], "</string>")
			if closeIdx < open {
				return "", false
			}
			return xmlUnescape(lines[j][open+len("<string>") : closeIdx]), true
		}
		return "", false
	}
	return "", false
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func xmlUnescape(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// plistProvenance classifies an existing plist for the DP-4b decision.
func plistProvenance(existing []byte) Provenance {
	_, prov := plistCommentStyle.split(existing)
	return prov
}
