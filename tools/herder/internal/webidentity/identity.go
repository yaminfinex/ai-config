// Package webidentity derives attributed web-peer senders from tailscale whois.
package webidentity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

const whoisTimeout = 5 * time.Second

var reservedUsers = map[string]bool{
	"bigboss": true, "conductor": true, "hcom": true, "herder": true,
	"owner": true, "system": true,
}

// Sender resolves the peer's tailnet login and renders a visibly web-origin
// hcom sender. Loopback is deliberately read-only under the pinned contract.
func Sender(ctx context.Context, remoteAddr string) (string, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return "", fmt.Errorf("cannot resolve tailnet identity for peer %q", remoteAddr)
	}
	whoisCtx, cancel := context.WithTimeout(ctx, whoisTimeout)
	defer cancel()
	cmd := exec.CommandContext(whoisCtx, "tailscale", "whois", "--json", ip.String())
	out, err := cmd.Output()
	if err != nil {
		if whoisCtx.Err() != nil {
			return "", fmt.Errorf("tailscale whois timed out: %w", whoisCtx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(bytes.TrimSpace(exitErr.Stderr)) > 0 {
			return "", fmt.Errorf("tailscale whois failed: %s", bytes.TrimSpace(exitErr.Stderr))
		}
		return "", fmt.Errorf("tailscale whois failed: %w", err)
	}
	var result struct {
		UserProfile struct {
			LoginName string `json:"LoginName"`
		} `json:"UserProfile"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("invalid tailscale whois JSON: %w", err)
	}
	login := strings.TrimSpace(result.UserProfile.LoginName)
	if login == "" {
		return "", errors.New("tailscale whois returned no user login")
	}
	user := slug(login)
	if user == "" {
		return "", errors.New("tailnet user login cannot form a bus sender")
	}
	local := user
	if index := strings.IndexByte(local, '-'); index >= 0 {
		local = local[:index]
	}
	if reservedUsers[user] || reservedUsers[local] {
		return "", fmt.Errorf("tailnet user %q is reserved as a bus sender", login)
	}
	return bounded("web-"+user, login), nil
}

func slug(value string) string {
	var result strings.Builder
	dash := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r <= unicode.MaxASCII {
				result.WriteRune(r)
			}
			dash = false
		} else if result.Len() > 0 && !dash {
			result.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(result.String(), "-")
}

func bounded(sender, source string) string {
	if len(sender) <= 50 {
		return sender
	}
	digest := sha256.Sum256([]byte(source))
	suffix := "-" + hex.EncodeToString(digest[:4])
	return strings.TrimRight(sender[:50-len(suffix)], "-") + suffix
}
