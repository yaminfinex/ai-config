package setup

import (
	"strings"
)

// The Linux node-local config surface is a systemd env drop-in
// (~/.config/systemd/user/sesh-ship.service.d/10-local.conf). It carries
// SESH_STORE_URL and may also carry operator env such as SESH_STATE_DIR, so a
// rewrite replaces only the SESH_STORE_URL assignment and preserves every
// other line — including comments, unknown keys, and their systemd quoting —
// byte-for-byte.

const storeURLKey = "SESH_STORE_URL"

// RenderDropin computes the drop-in content for storeURL and stamps it with
// the provenance digest. existing is the current file content (nil when the
// file does not exist); its lines are preserved verbatim except the
// SESH_STORE_URL assignment and the previous digest line.
func RenderDropin(existing []byte, storeURL string) []byte {
	body, _ := unitCommentStyle.split(existing)
	if len(body) == 0 {
		fresh := "# Written by sesh setup — node-local values only. Re-run `sesh setup` to change.\n" +
			"[Service]\n" +
			"Environment=" + storeURLKey + "=" + storeURL + "\n"
		return unitCommentStyle.stamp([]byte(fresh))
	}

	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	replaced := false
	lastServiceLine := -1
	inService := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inService = trimmed == "[Service]"
		}
		if inService {
			lastServiceLine = i
		}
		rewritten, ok := rewriteEnvironmentLine(line, storeURLKey, storeURL)
		if ok {
			lines[i] = rewritten
			replaced = true
		}
	}
	if !replaced {
		assignment := "Environment=" + storeURLKey + "=" + storeURL
		if lastServiceLine >= 0 {
			lines = append(lines[:lastServiceLine+1],
				append([]string{assignment}, lines[lastServiceLine+1:]...)...)
		} else {
			lines = append(lines, "[Service]", assignment)
		}
	}
	return unitCommentStyle.stamp([]byte(strings.Join(lines, "\n") + "\n"))
}

// DropinStoreURL extracts the SESH_STORE_URL value from drop-in content,
// honoring systemd's last-assignment-wins semantics.
func DropinStoreURL(content []byte) (string, bool) {
	var value string
	var found bool
	for _, line := range strings.Split(string(content), "\n") {
		rest, ok := environmentValue(line)
		if !ok {
			continue
		}
		for _, tok := range splitEnvTokens(rest) {
			unq := unquoteToken(tok)
			if v, ok := strings.CutPrefix(unq, storeURLKey+"="); ok {
				value, found = v, true
			}
		}
	}
	return value, found
}

// environmentValue returns the value part of an Environment= line.
func environmentValue(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
		return "", false
	}
	key, value, ok := strings.Cut(trimmed, "=")
	if !ok || strings.TrimSpace(key) != "Environment" {
		return "", false
	}
	return value, true
}

// rewriteEnvironmentLine replaces the assignment for key inside an
// Environment= line with newValue, preserving the line's other assignments
// and each token's quoting style. ok reports whether the line carried key.
func rewriteEnvironmentLine(line, key, newValue string) (string, bool) {
	value, isEnv := environmentValue(line)
	if !isEnv {
		return line, false
	}
	tokens := splitEnvTokens(value)
	replaced := false
	for i, tok := range tokens {
		unq := unquoteToken(tok)
		if !strings.HasPrefix(unq, key+"=") {
			continue
		}
		switch {
		case strings.HasPrefix(tok, `"`):
			tokens[i] = `"` + key + "=" + newValue + `"`
		case strings.HasPrefix(tok, `'`):
			tokens[i] = `'` + key + "=" + newValue + `'`
		default:
			tokens[i] = key + "=" + newValue
		}
		replaced = true
	}
	if !replaced {
		return line, false
	}
	eq := strings.Index(line, "=")
	return line[:eq+1] + strings.Join(tokens, " "), true
}

// splitEnvTokens splits a systemd Environment= value into its
// whitespace-separated assignments, keeping surrounding quotes on each token.
func splitEnvTokens(value string) []string {
	var tokens []string
	var current strings.Builder
	var quote byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case quote != 0:
			current.WriteByte(c)
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			current.WriteByte(c)
		case c == ' ' || c == '\t':
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// unquoteToken strips one matching pair of surrounding quotes.
func unquoteToken(tok string) string {
	if len(tok) >= 2 && (tok[0] == '"' || tok[0] == '\'') && tok[len(tok)-1] == tok[0] {
		return tok[1 : len(tok)-1]
	}
	return tok
}

// dropinProvenance classifies an existing drop-in for the DP-4b decision.
func dropinProvenance(existing []byte) Provenance {
	_, prov := unitCommentStyle.split(existing)
	return prov
}

// UnitExecPath extracts the pinned binary path from a rendered unit's
// ExecStart= line ("ExecStart=<abs path> ship"). `sesh update` replaces
// exactly this file — never a binary the service does not actually run.
func UnitExecPath(content []byte) (string, bool) {
	for _, line := range strings.Split(string(content), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "ExecStart=")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			return fields[0], true
		}
	}
	return "", false
}
