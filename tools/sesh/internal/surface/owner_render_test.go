package surface_test

// R15 render half: precedence verdicts on the actual pages — person
// grouping, source labels, conflict badges, honest absence. The precedence
// table itself is pinned in owner_test.go (package surface).

import (
	"strings"
	"testing"
	"time"

	"sesh/internal/wire"
)

func ownerSpec(t *testing.T, claims []string, tailnet string) sessionSpec {
	t.Helper()
	day := func(d string) time.Time {
		ts, err := time.Parse(time.RFC3339, d)
		if err != nil {
			t.Fatal(err)
		}
		return ts
	}
	return sessionSpec{
		tool: wire.ToolClaude, logicalID: uuidNormal,
		hostname: "workstation", osUser: "grace",
		ownerClaims: claims, tailnetIdentity: tailnet,
		mirroredAt: day("2026-07-03T10:05:00Z"),
		files:      []fixtureFile{{name: "claude-normal.jsonl", fileUUID: uuidNormal, firstIngest: day("2026-07-03T10:00:00Z")}},
	}
}

func TestOwnerClaimGroupsPersonWithSource(t *testing.T) {
	srv := newServer(t, buildStore(t, []sessionSpec{ownerSpec(t, []string{"alice"}, "")}))
	page := mustGet200(t, srv, "/")
	if !strings.Contains(page, "<h2>alice <span class=\"source\">SESSION_OWNER fact</span></h2>") {
		t.Error("claimed session must group under the owner with the SESSION_OWNER source label")
	}
	transcript := mustGet200(t, srv, "/s/claude/"+uuidNormal)
	if !strings.Contains(transcript, "alice") || !strings.Contains(transcript, "SESSION_OWNER fact") {
		t.Error("transcript meta must name the owner and its source")
	}
}

func TestConflictingClaimsRenderHonestAbsence(t *testing.T) {
	srv := newServer(t, buildStore(t, []sessionSpec{ownerSpec(t, []string{"carol", "dave"}, "")}))
	page := mustGet200(t, srv, "/")
	// Absence: neither claimant becomes a person; the session groups under
	// its node with the conflicting-claims badge.
	for _, name := range []string{"carol", "dave"} {
		if strings.Contains(page, name) {
			t.Errorf("conflicting claimant %q rendered on the recency page; conflict must render absence", name)
		}
	}
	if !strings.Contains(page, "grace@workstation") {
		t.Error("conflicted session must group under node/OS-user")
	}
	if !strings.Contains(page, "conflicting claims") {
		t.Error("conflicted session row must carry the conflicting-claims badge")
	}
	transcript := mustGet200(t, srv, "/s/claude/"+uuidNormal)
	if !strings.Contains(transcript, "conflicting SESSION_OWNER claims") {
		t.Error("transcript meta must label the conflict")
	}
	if strings.Contains(transcript, "carol") || strings.Contains(transcript, "dave") {
		t.Error("transcript meta must not pick or print a conflicting claimant")
	}
}

func TestTailnetIdentityTierWinsWhenNoClaim(t *testing.T) {
	// AC3, M4 half: a facts-only session (macOS shape — no SESSION_OWNER)
	// with a store-stamped tailnet identity groups under that identity.
	srv := newServer(t, buildStore(t, []sessionSpec{ownerSpec(t, nil, "bob@tailnet")}))
	page := mustGet200(t, srv, "/")
	if !strings.Contains(page, "<h2>bob@tailnet <span class=\"source\">tailnet identity</span></h2>") {
		t.Error("tailnet identity must win and be labeled when no SESSION_OWNER claim exists")
	}
}

func TestUnclaimedFallsThroughToNodeGrouping(t *testing.T) {
	// AC3, pre-M4 half: no claim, no tailnet identity → the session groups
	// honestly under node/OS-user; the OS-user tier labels the transcript.
	srv := newServer(t, buildStore(t, []sessionSpec{ownerSpec(t, nil, "")}))
	page := mustGet200(t, srv, "/")
	if !strings.Contains(page, "<h2>grace@workstation <span class=\"source\">no owner claim — OS user @ host</span></h2>") {
		t.Error("unclaimed session must group under node/OS-user with the honest label")
	}
	transcript := mustGet200(t, srv, "/s/claude/"+uuidNormal)
	if !strings.Contains(transcript, "OS user (no owner claim)") {
		t.Error("transcript meta must label the OS-user attribution tier")
	}
}
