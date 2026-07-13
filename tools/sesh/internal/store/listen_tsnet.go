package store

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"

	"sesh/internal/wire"
)

const (
	CapabilityShip = "ship"
	CapabilityRead = "read"
)

const TailnetCapabilitySesh tailcfg.PeerCapability = "infinex.xyz/cap/sesh"

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

// WhoIsResult is the authenticated tailnet caller plus the app capabilities
// Tailscale resolved for this connection.
type WhoIsResult struct {
	Identity string
	CapMap   tailcfg.PeerCapMap
}

// WhoIsFunc maps a connection's remote address to its authenticated tailnet
// identity and peer capability map.
type WhoIsFunc func(context.Context, string) (WhoIsResult, error)

// TailnetCapabilityGrant is the JSON value stored under
// TailnetCapabilitySesh in Tailscale grants.
type TailnetCapabilityGrant struct {
	Verb  string   `json:"verb,omitempty"`
	Verbs []string `json:"verbs,omitempty"`
}

// AuthHandler stamps the WhoIs identity into context and enforces the grant
// before delegating. It is used for both ship and read listeners; loopback dev
// mode bypasses it entirely.
func AuthHandler(next http.Handler, whois WhoIsFunc, capability string) http.Handler {
	return AuthHandlerAnyOf(next, whois, capability)
}

// AuthHandlerAnyOf admits a caller holding ANY of the given verbs. The
// distribution routes on the ingest listener use it with (ship, read) —
// design §3's route-scoped any-of-verbs auth — so read-only principals can
// fetch the installer and releases without gaining PUT ingest.
func AuthHandlerAnyOf(next http.Handler, whois WhoIsFunc, verbs ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if whois == nil {
			writeGrantDenied(w, "tailnet identity unavailable")
			return
		}
		result, err := whois(r.Context(), r.RemoteAddr)
		if err != nil || result.Identity == "" {
			if err == nil {
				err = fmt.Errorf("empty tailnet identity")
			}
			writeGrantDenied(w, err.Error())
			return
		}
		allowed, err := allowsAnyCapability(result.CapMap, verbs)
		if err != nil {
			writeGrantDenied(w, err.Error())
			return
		}
		if !allowed {
			writeGrantDenied(w, fmt.Sprintf("tailnet identity %q lacks %s verb in %s", result.Identity, strings.Join(verbs, "|"), TailnetCapabilitySesh))
			return
		}
		next.ServeHTTP(w, r.WithContext(withTailnetIdentity(r.Context(), result.Identity)))
	})
}

func allowsAnyCapability(caps tailcfg.PeerCapMap, verbs []string) (bool, error) {
	grants, err := tailcfg.UnmarshalCapJSON[TailnetCapabilityGrant](caps, TailnetCapabilitySesh)
	if err != nil {
		return false, fmt.Errorf("invalid %s grant: %w", TailnetCapabilitySesh, err)
	}
	for _, grant := range grants {
		for _, verb := range verbs {
			if grant.Verb == verb {
				return true, nil
			}
			for _, granted := range grant.Verbs {
				if granted == verb {
					return true, nil
				}
			}
		}
	}
	return false, nil
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

func (s *TSNetServer) WhoIs(ctx context.Context, remoteAddr string) (WhoIsResult, error) {
	lc, err := s.srv.LocalClient()
	if err != nil {
		return WhoIsResult{}, err
	}
	who, err := lc.WhoIs(ctx, remoteAddr)
	if err != nil {
		return WhoIsResult{}, err
	}
	var identity string
	if who.UserProfile != nil && who.UserProfile.LoginName != "" {
		identity = who.UserProfile.LoginName
	} else if who.Node != nil && who.Node.Name != "" {
		identity = who.Node.Name
	}
	if identity == "" {
		return WhoIsResult{}, fmt.Errorf("WhoIs returned no user or node identity for %s", remoteAddr)
	}
	return WhoIsResult{Identity: identity, CapMap: who.CapMap}, nil
}

func (s *TSNetServer) Close() error {
	if s == nil || s.srv == nil {
		return nil
	}
	return s.srv.Close()
}

var _ io.Closer = (*TSNetServer)(nil)
