package surface

import "testing"

// The R15 precedence table: each tier wins exactly when every higher tier
// is absent, the label names the source, conflicts render honest absence.
func TestDisplayOwnerPrecedence(t *testing.T) {
	cases := []struct {
		name string
		sum  SessionSummary
		want DisplayOwner
	}{
		{
			name: "SESSION_OWNER wins over everything",
			sum:  SessionSummary{OwnerClaims: []string{"alice"}, TailnetIdentity: "ts-bob", OSUser: "grace", Hostname: "ws"},
			want: DisplayOwner{Name: "alice", Source: "SESSION_OWNER fact", Claimed: true},
		},
		{
			name: "repeated identical claims are one claim, not a conflict",
			sum:  SessionSummary{OwnerClaims: []string{"alice", "alice", "alice"}},
			want: DisplayOwner{Name: "alice", Source: "SESSION_OWNER fact", Claimed: true},
		},
		{
			name: "conflicting claims render honest absence with the label",
			sum:  SessionSummary{OwnerClaims: []string{"carol", "dave"}, TailnetIdentity: "ts-bob", OSUser: "grace"},
			want: DisplayOwner{Conflict: true, Source: "conflicting SESSION_OWNER claims"},
		},
		{
			name: "tailnet identity wins when no SESSION_OWNER (macOS facts-only at M4)",
			sum:  SessionSummary{TailnetIdentity: "ts-bob", OSUser: "grace", Hostname: "mba"},
			want: DisplayOwner{Name: "ts-bob", Source: "tailnet identity", Claimed: true},
		},
		{
			name: "OS user is the pre-M4 floor for facts-only sessions",
			sum:  SessionSummary{OSUser: "grace", Hostname: "mba"},
			want: DisplayOwner{Name: "grace", Source: "OS user (no owner claim)"},
		},
		{
			name: "hostname is the absolute floor",
			sum:  SessionSummary{Hostname: "ws"},
			want: DisplayOwner{Name: "ws", Source: "hostname (no owner claim)"},
		},
		{
			name: "no facts at all stays honest",
			sum:  SessionSummary{},
			want: DisplayOwner{Source: "no attribution facts"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.sum.DisplayOwner()
			if got != tc.want {
				t.Errorf("DisplayOwner() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
