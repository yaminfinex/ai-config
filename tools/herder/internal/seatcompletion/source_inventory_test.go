package seatcompletion

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestSeatCompletionOwnsSoleProductionLaunchContextRepairCall(t *testing.T) {
	files := productionInternalGoFiles(t)
	var callers []string
	for path, source := range files {
		for range strings.Count(source, ".RepairLaunchContext(") {
			callers = append(callers, path)
		}
	}
	sort.Strings(callers)
	want := []string{"seatcompletion/completion.go"}
	if strings.Join(callers, "\n") != strings.Join(want, "\n") {
		t.Fatalf("RepairLaunchContext production callers = %v, want %v", callers, want)
	}
}

func TestLockedCompletionFinalizerHasExactlyCredentialCommandCaller(t *testing.T) {
	files := productionInternalGoFiles(t)
	var finalizerCallers []string
	for path, source := range files {
		usage, err := completionArmUsage(source)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if usage.finalizeLocked {
			finalizerCallers = append(finalizerCallers, path)
		}
	}
	sort.Strings(finalizerCallers)
	wantFinalizers := []string{"credentialcmd/credential.go"}
	if strings.Join(finalizerCallers, "\n") != strings.Join(wantFinalizers, "\n") {
		t.Fatalf("locked completion finalizer production callers = %v, want %v", finalizerCallers, wantFinalizers)
	}
}

func TestCompletionArmInventoryDetectsAlternateForms(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
		want completionArmInventory
	}{
		{
			name: "finalizer assignment",
			src:  `package p; func f(req *seatcompletion.Request) { req.FinalizeLocked = finish }`,
			want: completionArmInventory{finalizeLocked: true},
		},
		{
			name: "finalizer request composite",
			src:  `package p; var _ = seatcompletion.Request{FinalizeLocked: finish}`,
			want: completionArmInventory{finalizeLocked: true},
		},
		{
			name: "reads do not arm",
			src:  `package p; func f(req seatcompletion.Request) { _ = req.FinalizeLocked }`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := completionArmUsage(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("completionArmUsage() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSeatRewriteWriterInventoryRequiresCarryPins(t *testing.T) {
	files := productionInternalGoFiles(t)
	type writerInventory struct {
		updateLocked      int
		completionRequest int
		carryPin          bool
	}
	writers := map[string]writerInventory{
		"adoptcmd/adopt.go":           {completionRequest: 1},
		"credentialcmd/credential.go": {completionRequest: 1},
		"cullcmd/cull.go":             {updateLocked: 1},
		"enrollcmd/enroll.go":         {completionRequest: 1},
		"grokbridge/binder.go":        {updateLocked: 1, carryPin: true},
		"grokbridge/completion.go":    {updateLocked: 1, completionRequest: 1, carryPin: true},
		"lifecyclecmd/lifecycle.go":   {updateLocked: 2, completionRequest: 1, carryPin: true},
		"liveness/apply.go":           {updateLocked: 1},
		"missioncmd/mission.go":       {updateLocked: 1, carryPin: true},
		"observercmd/observer.go":     {updateLocked: 1, completionRequest: 1, carryPin: true},
		"renamecmd/rename.go":         {updateLocked: 2, carryPin: true},
		"retirecmd/retire.go":         {updateLocked: 2},
		"sidecarcmd/sidecar.go":       {completionRequest: 1, carryPin: true},
		"spawncmd/compact.go":         {updateLocked: 1, carryPin: true},
		"spawncmd/spawn.go":           {updateLocked: 1, completionRequest: 1, carryPin: true},
	}
	directWant := map[string]int{}
	completionWant := map[string]int{}
	for source, inventory := range writers {
		if inventory.updateLocked > 0 {
			directWant[source] = inventory.updateLocked
		}
		if inventory.completionRequest > 0 {
			completionWant[source] = inventory.completionRequest
		}
	}
	assertCallInventory(t, files, "registry.UpdateLocked(", directWant)
	assertCallInventory(t, files, "seatcompletion.Request{", completionWant)
	assertCallInventory(t, files, "registry.Append(", map[string]int{})
	assertCallInventory(t, files, "registry.AppendLegacySessionEvent(", map[string]int{})

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve seat writer pin paths")
	}
	internal := filepath.Dir(filepath.Dir(thisFile))
	registryPins, err := os.ReadFile(filepath.Join(internal, "registry", "seat_carry_test.go"))
	if err != nil {
		t.Fatalf("read structural carry pins: %v", err)
	}
	for source, inventory := range writers {
		if !inventory.carryPin {
			continue
		}
		marker := `source: "` + source + `"`
		if !strings.Contains(string(registryPins), marker) {
			t.Fatalf("seat writer %s is inventory-pinned for carry but lacks a structural UpdateLocked pin", source)
		}
	}
	pins := []struct {
		path   string
		marker string
	}{
		{path: "seatcompletion/completion_test.go", marker: "TestCompletionRotatesPersistedCredentialGeneration"},
		{path: "registry/seat_carry_test.go", marker: "TestSeatedRewriteEventInventoryCarriesUnownedSeatFacts"},
		{path: "registry/seat_carry_test.go", marker: "TestSeatedNilSeatAppendCannotErasePersistedSeat"},
		{path: "registry/seat_carry_test.go", marker: "TestSeatRewriteWriterPinsDependOnStructuralCarry"},
		{path: "registry/seat_carry_test.go", marker: "TestCompatibilityAppendWritersCarryCredentialGeneration"},
		{path: "../tests/check-enroll-contract.sh", marker: `credential_generation":"[0-9a-f]`},
	}
	for _, pin := range pins {
		raw, err := os.ReadFile(filepath.Join(internal, filepath.FromSlash(pin.path)))
		if err != nil {
			t.Fatalf("read carry pin %s: %v", pin.path, err)
		}
		if !strings.Contains(string(raw), pin.marker) {
			t.Fatalf("seat rewrite carry pin %s lost marker %q", pin.path, pin.marker)
		}
	}

	registrySource := files["registry/registry.go"]
	for _, entryPoint := range []string{"func Append(", "func AppendLegacySessionEvent("} {
		if strings.Count(registrySource, entryPoint) != 1 {
			t.Fatalf("compatibility append entry point %q changed; inventory and pin its seated carry behavior", entryPoint)
		}
	}
}

func assertCallInventory(t *testing.T, files map[string]string, needle string, want map[string]int) {
	t.Helper()
	got := map[string]int{}
	for path, source := range files {
		if count := strings.Count(source, needle); count > 0 {
			got[path] = count
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("production call inventory for %q = %v, want %v; add a writer carry pin before updating this inventory", needle, got, want)
	}
}

type completionArmInventory struct {
	finalizeLocked bool
}

func completionArmUsage(source string) (completionArmInventory, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "inventory.go", source, 0)
	if err != nil {
		return completionArmInventory{}, err
	}
	var usage completionArmInventory
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CompositeLit:
			for _, element := range node.Elts {
				keyValue, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := keyValue.Key.(*ast.Ident); ok {
					markCompletionArm(&usage, key.Name)
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				if selector, ok := lhs.(*ast.SelectorExpr); ok {
					markCompletionArm(&usage, selector.Sel.Name)
				}
			}
		}
		return true
	})
	return usage, nil
}

func markCompletionArm(usage *completionArmInventory, name string) {
	switch name {
	case "FinalizeLocked":
		usage.finalizeLocked = true
	}
}

func productionInternalGoFiles(t *testing.T) map[string]string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate source tree")
	}
	root := filepath.Dir(filepath.Dir(thisFile))
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
