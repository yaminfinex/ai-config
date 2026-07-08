package send

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
)

// stubJoined installs a mock `hcom` on PATH whose `list <name>` exits 0 only
// for the names in STUB_JOINED (space-separated) — the bus-liveness signal
// disambiguatePane keys on.
func stubJoined(t *testing.T, joined string) {
	t.Helper()
	dir := t.TempDir()
	stub := `#!/usr/bin/env bash
case "$1" in
  list)
    for n in $STUB_JOINED; do [[ "$n" == "$2" ]] && exit 0; done
    exit 1;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STUB_JOINED", joined)
}

func sp(s string) *string { return &s }

func paneCandidates() []registry.Record {
	return []registry.Record{
		{GUID: sp("guid-hera-0000"), Label: sp("hera"), PaneID: "p_1", HcomName: "hera-rive", Status: "active"},
		{GUID: sp("guid-vore-0000"), Label: sp("vore"), PaneID: "p_1", HcomName: "vore-lilo", Status: "active"},
		{GUID: sp("guid-zero-0000"), Label: sp("zero"), PaneID: "p_1", HcomName: "zero-mano", Status: "active"},
	}
}

func TestDisambiguatePaneOneLiveWins(t *testing.T) {
	// One live (@hera) among two stale — deliver to the live one, no error.
	stubJoined(t, "hera-rive")
	var stderr bytes.Buffer
	chosen, code := disambiguatePane(&busSender{}, paneCandidates(), "p_1", &stderr)
	if chosen == nil || ptrString(chosen.GUID) != "guid-hera-0000" {
		t.Fatalf("chosen = %+v (code %d), want hera", chosen, code)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr on clean resolution: %q", stderr.String())
	}
}

func TestDisambiguatePaneNoneLiveRefuses(t *testing.T) {
	// Every candidate stale (none joined) — refuse with exit 2 and the full
	// candidate list; never silently pick.
	stubJoined(t, "")
	var stderr bytes.Buffer
	chosen, code := disambiguatePane(&busSender{}, paneCandidates(), "p_1", &stderr)
	if chosen != nil || code != 2 {
		t.Fatalf("chosen=%+v code=%d, want nil/2", chosen, code)
	}
	msg := stderr.String()
	for _, want := range []string{"none is joined", "guid-hera-0000", "guid-vore-0000", "guid-zero-0000", "Nothing was sent"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal missing %q; got: %s", want, msg)
		}
	}
}

func TestDisambiguatePaneMultipleLiveRefuses(t *testing.T) {
	// Two joined at once — genuine ambiguity, refuse and list the LIVE rows.
	stubJoined(t, "hera-rive vore-lilo")
	var stderr bytes.Buffer
	chosen, code := disambiguatePane(&busSender{}, paneCandidates(), "p_1", &stderr)
	if chosen != nil || code != 2 {
		t.Fatalf("chosen=%+v code=%d, want nil/2", chosen, code)
	}
	msg := stderr.String()
	if !strings.Contains(msg, "bus-live at once") {
		t.Errorf("refusal missing ambiguity phrasing; got: %s", msg)
	}
	if !strings.Contains(msg, "guid-hera-0000") || !strings.Contains(msg, "guid-vore-0000") {
		t.Errorf("refusal should list both live rows; got: %s", msg)
	}
	if strings.Contains(msg, "guid-zero-0000") {
		t.Errorf("refusal listed the non-live zero row; got: %s", msg)
	}
}
