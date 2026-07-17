package liveness

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLivenessInferenceSourceInventory(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve source inventory path")
	}
	internal := filepath.Dir(filepath.Dir(thisFile))
	targets := map[string][]string{
		"observercmd/observer.go": {
			"busCorroboratesDead", "processDead(", "occupantGone(", "StatusAge >", "positive epoch/bus evidence",
		},
		"sidecarcmd/sidecar.go":     {"StatusAgeS >", "s.missing >= 5 {\n\t\t\ts.release"},
		"cullcmd/cull.go":           {"terminal_id not in live agent list"},
		"listcmd/list.go":           {"return \"gone\""},
		"reconcilecmd/reconcile.go": {"Outcome = \"gone\""},
		"waitcmd/wait.go":           {"gone or culled"},
	}
	for rel, forbidden := range targets {
		b, err := os.ReadFile(filepath.Join(internal, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := string(b)
		if !strings.Contains(text, "liveness.") {
			t.Fatalf("%s no longer uses the shared liveness package", rel)
		}
		for _, needle := range forbidden {
			if strings.Contains(text, needle) {
				t.Fatalf("%s reintroduced deleted liveness inference %q", rel, needle)
			}
		}
	}
}

func TestObservedDeathWritesStayBehindSharedApplier(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	internal := filepath.Dir(filepath.Dir(thisFile))
	err := filepath.WalkDir(internal, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(b), `CloseResult = "observed_dead"`) && filepath.Base(path) != "apply.go" {
			t.Errorf("%s writes observed_dead outside the shared applier", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPaneHuskInferenceCannotBecomeDeathPath(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve liveness source path")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "liveness.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, forbidden := range []string{"CauseOccupantExited", "foreground_process_exited"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("one-sweep pane husk inference remains a death path via %q", forbidden)
		}
	}
	start := strings.Index(text, "func deathFacts(")
	end := strings.Index(text, "func gapFacts(")
	if start < 0 || end <= start {
		t.Fatal("cannot isolate deathFacts source inventory")
	}
	deathFacts := text[start:end]
	if count := strings.Count(deathFacts, "return Cause"); count != 3 {
		t.Fatalf("deathFacts has %d positive-death returns, want the settled three-path fence", count)
	}
	for _, required := range []string{"return CauseHolderExited", "return CausePaneGoneSameEpoch", "return CauseDeadPIDStaleBusRow"} {
		if !strings.Contains(deathFacts, required) {
			t.Fatalf("deathFacts lost settled path %q", required)
		}
	}
}

func TestRepairUsesLivenessOnlyInClassificationSurface(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	internal := filepath.Dir(filepath.Dir(thisFile))
	repair, err := os.ReadFile(filepath.Join(internal, "repaircmd", "repair.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(repair), "internal/liveness") || strings.Contains(string(repair), "liveness.") {
		t.Fatal("repair mutation arm imported liveness; classification must remain ceremony-only")
	}
	ceremony, err := os.ReadFile(filepath.Join(internal, "repaircmd", "ceremony.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ceremony), "repairLivenessGap") {
		t.Fatal("repair classification surface lost shared observation-gap advice")
	}
}
