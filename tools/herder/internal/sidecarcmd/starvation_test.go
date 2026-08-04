package sidecarcmd

import (
	"strings"
	"testing"
	"time"
)

func TestStarvationExitsAfterBoundedWindowWhenNoOwnedProcess(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-starved-exit")
	var diagnostic strings.Builder
	s := &sidecar{
		tool:                "claude",
		diagnostic:          &diagnostic,
		starvationExitAfter: -1, // window elapses immediately once starved
		processEnvirons: func(string) []processEnvironmentRead {
			return nil
		},
	}
	for poll := 0; poll < 4; poll++ {
		if !s.observeLiveness(false, nil) {
			t.Fatalf("sidecar exited on poll %d before starvation threshold", poll)
		}
	}
	if s.observeLiveness(false, nil) {
		t.Fatal("starved seat with no owned process did not exit after the bounded window")
	}
	out := diagnostic.String()
	if !strings.Contains(out, "exiting after") || !strings.Contains(out, "starvation") {
		t.Fatalf("exit reason not recorded, diagnostics=%q", out)
	}
}

func TestStarvationKeepsWatchWhileOwnedProcessLives(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-starved-alive")
	s := &sidecar{
		tool:                "claude",
		diagnostic:          &strings.Builder{},
		starvationExitAfter: -1,
		processEnvirons: func(string) []processEnvironmentRead {
			return []processEnvironmentRead{{pid: 4242, env: map[string]string{
				"HERDER_GUID": "guid-starved-alive", "HCOM_INSTANCE_NAME": "mine",
			}}}
		},
	}
	for poll := 0; poll < 10; poll++ {
		if !s.observeLiveness(false, nil) {
			t.Fatalf("sidecar abandoned a starved seat whose owned process is alive (poll %d)", poll)
		}
	}
}

func TestStarvationClockResetsWhenRowReturns(t *testing.T) {
	s := &sidecar{diagnostic: &strings.Builder{}, starvationExitAfter: -1}
	for poll := 0; poll < 4; poll++ {
		if !s.observeLiveness(false, nil) {
			t.Fatal("premature exit")
		}
	}
	fresh := &hcomRow{Name: "back", StatusAgeS: 0}
	if !s.observeLiveness(false, fresh) {
		t.Fatal("fresh row did not keep sidecar alive")
	}
	if !s.starvedSince.IsZero() {
		t.Fatal("starvation clock not reset by fresh row")
	}
}

func TestStarvationUsesAgedKeepaliveFromCachedRow(t *testing.T) {
	var diagnostic strings.Builder
	s := &sidecar{diagnostic: &diagnostic}
	// Row fetched long ago with a near-threshold age: local aging must push it
	// over the 5-minute keepalive starvation line without a new fetch.
	s.rowsFetchedAt = time.Now().Add(-2 * time.Minute)
	stale := &hcomRow{Name: "quiet", StatusAgeS: 4 * 60}
	if !s.observeLiveness(false, stale) {
		t.Fatal("advisory starvation must not exit within the window")
	}
	if !strings.Contains(diagnostic.String(), "starved") {
		t.Fatalf("aged cached row did not trigger starvation advisory, diagnostics=%q", diagnostic.String())
	}
	if s.starvedSince.IsZero() {
		t.Fatal("starvation clock did not start from aged cached row")
	}
}
