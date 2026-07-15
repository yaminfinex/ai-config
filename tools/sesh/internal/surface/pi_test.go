package surface_test

import (
	"strings"
	"testing"
	"time"

	"sesh/internal/wire"
)

func TestPiT27RendersActiveBranchAndLabelsBranchPoint(t *testing.T) {
	srv := newServer(t, piStore(t))
	body := mustGet200(t, srv, "/s/pi/"+uuidPiBranched)
	for _, want := range []string{"ACTIVE-BRANCH-CONTENT", "branch point:", "chosen option", "active path"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Pi T27 render missing %q", want)
		}
	}
	if strings.Contains(body, "ABANDONED-BRANCH-CONTENT") {
		t.Fatal("Pi T27 render silently flattened an inactive branch")
	}
}

func TestPiNever500OnMalformedAndEmptyTrees(t *testing.T) {
	when, _ := time.Parse(time.RFC3339, "2026-07-15T12:35:30Z")
	for name, data := range map[string][]byte{
		"malformed": []byte("{broken\n"),
		"empty":     {},
	} {
		t.Run(name, func(t *testing.T) {
			store := buildStore(t, []sessionSpec{{
				tool: wire.ToolPi, logicalID: uuidPiBranched,
				hostname: "node", osUser: "user", mirroredAt: when,
				files: []fixtureFile{{bytes: data, fileUUID: uuidPiBranched, firstIngest: when}},
			}})
			srv := newServer(t, store)
			body := mustGet200(t, srv, "/s/pi/"+uuidPiBranched)
			if !strings.Contains(body, "Pi branch graph") && !strings.Contains(body, "Pi transcript is empty") && !strings.Contains(body, "raw mirror lines") {
				t.Fatalf("degraded Pi render did not label its floor: %s", body)
			}
		})
	}
}
