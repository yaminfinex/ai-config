package store

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"tailscale.com/tsnet"

	"sesh/internal/wire"
)

const (
	CapabilityShip = "ship"
	CapabilityRead = "read"
)

type contextKeyTailnetIdentity struct{}

// TailnetIdentityFromContext returns the store-stamped WhoIs identity for this
// request, or empty in loopback development mode.
func TailnetIdentityFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyTailnetIdentity{}).(string)
	return v
}

func withTailnetIdentity(ctx context.Context, identity string) context.Context {
	if identity == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKeyTailnetIdentity{}, identity)
}

// WhoIsFunc maps a connection's remote address to the tailnet identity
// authenticated by tailscaled/tsnet.
type WhoIsFunc func(context.Context, string) (string, error)

// GrantPolicy is the M4 capability gate: an identity must be listed for the
// capability it is attempting, or the request is denied before the handler
// sees bytes or renders reads.
type GrantPolicy struct {
	Ship map[string]bool
	Read map[string]bool
}

func NewGrantPolicy(shipCSV, readCSV string) GrantPolicy {
	return GrantPolicy{
		Ship: grantSet(shipCSV),
		Read: grantSet(readCSV),
	}
}

func grantSet(csv string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func (g GrantPolicy) Allows(identity, capability string) bool {
	var set map[string]bool
	switch capability {
	case CapabilityShip:
		set = g.Ship
	case CapabilityRead:
		set = g.Read
	default:
		return false
	}
	return set["*"] || set[identity]
}

func (g GrantPolicy) Empty() bool {
	return len(g.Ship) == 0 && len(g.Read) == 0
}

// AuthHandler stamps the WhoIs identity into context and enforces the grant
// before delegating. It is used for both ship and read listeners; loopback dev
// mode bypasses it entirely.
func AuthHandler(next http.Handler, whois WhoIsFunc, grants GrantPolicy, capability string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if whois == nil {
			writeGrantDenied(w, "tailnet identity unavailable")
			return
		}
		identity, err := whois(r.Context(), r.RemoteAddr)
		if err != nil || identity == "" {
			if err == nil {
				err = fmt.Errorf("empty tailnet identity")
			}
			writeGrantDenied(w, err.Error())
			return
		}
		if !grants.Allows(identity, capability) {
			writeGrantDenied(w, fmt.Sprintf("tailnet identity %q is outside the %s grant", identity, capability))
			return
		}
		next.ServeHTTP(w, r.WithContext(withTailnetIdentity(r.Context(), identity)))
	})
}

func writeGrantDenied(w http.ResponseWriter, message string) {
	writeJSON(w, wire.ErrOutOfGrant.HTTPStatus(), wire.ErrorResponse{
		WireVersion: wire.Version,
		Code:        wire.ErrOutOfGrant,
		Message:     message,
	})
}

type TSNetOptions struct {
	Hostname string
	Dir      string
	AuthKey  string
}

// TSNetServer owns the embedded Tailscale node and exposes listeners plus a
// WhoIs function for auth wrapping.
type TSNetServer struct {
	srv *tsnet.Server
}

func NewTSNetServer(opts TSNetOptions) *TSNetServer {
	return &TSNetServer{srv: &tsnet.Server{
		Hostname: opts.Hostname,
		Dir:      opts.Dir,
		AuthKey:  opts.AuthKey,
	}}
}

func (s *TSNetServer) Listen(network, addr string) (net.Listener, error) {
	return s.srv.Listen(network, addr)
}

func (s *TSNetServer) WhoIs(ctx context.Context, remoteAddr string) (string, error) {
	lc, err := s.srv.LocalClient()
	if err != nil {
		return "", err
	}
	who, err := lc.WhoIs(ctx, remoteAddr)
	if err != nil {
		return "", err
	}
	if who.UserProfile.LoginName != "" {
		return who.UserProfile.LoginName, nil
	}
	if who.Node.Name != "" {
		return who.Node.Name, nil
	}
	return "", fmt.Errorf("WhoIs returned no user or node identity for %s", remoteAddr)
}

func (s *TSNetServer) Close() error {
	if s == nil || s.srv == nil {
		return nil
	}
	return s.srv.Close()
}

var _ io.Closer = (*TSNetServer)(nil)
