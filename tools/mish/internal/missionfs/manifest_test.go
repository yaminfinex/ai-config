package missionfs

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestManifestRoundTripAndFindings(t *testing.T) {
	dir := t.TempDir()
	missionDir := filepath.Join(dir, "perf-regression")
	if err := os.Mkdir(missionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Mission:   "perf-regression",
		Authority: "hera",
		Owner:     "riley",
		Status:    "active",
		Created:   "2026-07-08",
	}
	if err := WriteManifest(filepath.Join(missionDir, manifestName), manifest, "Perf regression hunt"); err != nil {
		t.Fatal(err)
	}

	got, findings, err := ReadManifest(missionDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != manifest {
		t.Fatalf("manifest round trip = %#v, want %#v", got, manifest)
	}
	if len(findings) != 0 {
		t.Fatalf("clean manifest findings = %#v, want none", findings)
	}

	data, err := os.ReadFile(filepath.Join(missionDir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mission:", "authority:", "owner:", "status:", "created:"} {
		if !slices.ContainsFunc(manifestKeys, func(key string) bool { return want == key+":" }) {
			t.Fatalf("test key %q is not pinned in manifestKeys", want)
		}
		if !strings.Contains(string(data), want) {
			t.Fatalf("written manifest missing %q:\n%s", want, data)
		}
	}
}

func TestManifestWarnsOnUnknownKeySlugMismatchAndInvalidStatus(t *testing.T) {
	dir := t.TempDir()
	missionDir := filepath.Join(dir, "perf-regression")
	if err := os.Mkdir(missionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(missionDir, manifestName), `---
mission: other-slug
authority: hera
owner: riley
status: parked
created: 2026-07-08
extra: value
---

# Other
`)

	_, findings, err := ReadManifest(missionDir)
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, findings, FindingUnknownManifestKey, "extra")
	assertFinding(t, findings, FindingManifestSlugMismatch, "mission")
	assertFinding(t, findings, FindingInvalidManifestStatus, "status")
}

func TestManifestWarnsOnMissingRequiredKeys(t *testing.T) {
	dir := t.TempDir()
	missionDir := filepath.Join(dir, "perf-regression")
	if err := os.Mkdir(missionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(missionDir, manifestName), `---
mission: perf-regression
created: 2026-07-08
---

# Perf regression
`)

	_, findings, err := ReadManifest(missionDir)
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, findings, FindingMissingManifestKey, "authority")
	assertFinding(t, findings, FindingMissingManifestKey, "owner")
	assertFinding(t, findings, FindingMissingManifestKey, "status")
}
