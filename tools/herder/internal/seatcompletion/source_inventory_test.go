package seatcompletion

import (
	"os"
	"path/filepath"
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

func TestAttestedCompletionArmHasNoProductionCaller(t *testing.T) {
	files := productionInternalGoFiles(t)
	var callers []string
	for path, source := range files {
		if path == "seatcompletion/completion.go" {
			continue
		}
		if strings.Contains(source, "Attested:") || strings.Contains(source, "AttestedBinding{") {
			callers = append(callers, path)
		}
	}
	sort.Strings(callers)
	if len(callers) != 0 {
		t.Fatalf("attested completion production callers = %v, want none", callers)
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
