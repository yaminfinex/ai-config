package enrollcmd

import (
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func rec(terminalID, hcomName string) registry.Record {
	return registry.Record{TerminalID: terminalID, HcomName: hcomName}
}

func TestLabelOwnerErrorDistinguishesActiveAndDeadHolders(t *testing.T) {
	active := labelOwnerError("stable", v2.SessionRecord{GUID: "guid-active", State: v2.StateSeated})
	if !strings.Contains(active.Error(), "already belongs to active guid guid-active") || strings.Contains(active.Error(), "herder adopt") {
		t.Fatalf("active error = %q", active)
	}
	dead := labelOwnerError("stable", v2.SessionRecord{GUID: "guid-dead", State: v2.StateUnseated})
	for _, want := range []string{"state unseated", "dead/unseated", "herder adopt guid-dead", "herder retire guid-dead", "herder rename <target> stable"} {
		if !strings.Contains(dead.Error(), want) {
			t.Fatalf("dead error = %q, want %q", dead, want)
		}
	}
}

// TestShouldRetirePriorRow pins TASK-035 P1-b: retire-on-reenroll must not
// close a row that could be a different, still-live session sharing a
// moved/reshuffled pane_id. terminal_id is the move-stable coordinate; a joined
// bus name is definitionally live.
func TestShouldRetirePriorRow(t *testing.T) {
	never := func(string, string) bool { return false }
	always := func(string, string) bool { return true }

	cases := []struct {
		name       string
		prior      registry.Record
		paneTermID string
		joined     func(string, string) bool
		want       bool
	}{
		{"same terminal, not joined -> retire", rec("term_A", "bus_a"), "term_A", never, true},
		{"different terminal both present -> keep (re-key guard)", rec("term_A", "bus_a"), "term_B", never, false},
		{"different terminal but joined -> keep", rec("term_A", "bus_a"), "term_B", always, false},
		{"same terminal but currently joined -> keep (live)", rec("term_A", "bus_a"), "term_A", always, false},
		{"prior has no terminal -> falls through to retire", rec("", "bus_a"), "term_B", never, true},
		{"enrolling pane has no terminal -> retire", rec("term_A", "bus_a"), "", never, true},
		{"bus-less prior, same terminal -> retire (probe skipped)", rec("term_A", ""), "term_A", always, true},
	}
	for _, c := range cases {
		if got := shouldRetirePriorRow(c.prior, c.paneTermID, c.joined); got != c.want {
			t.Errorf("%s: shouldRetirePriorRow = %v, want %v", c.name, got, c.want)
		}
	}
}
