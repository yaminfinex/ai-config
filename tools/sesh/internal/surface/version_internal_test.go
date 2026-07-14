package surface

import "testing"

func TestParseVersionForms(t *testing.T) {
	cases := []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"0.1.8", [3]int{0, 1, 8}, true},
		{"v0.1.8", [3]int{0, 1, 8}, true},
		{"sesh-v0.1.8", [3]int{0, 1, 8}, true},
		{"sesh-v0.1.8-3-g1a2b3c4", [3]int{0, 1, 8}, true},
		{"sesh-v0.1.8-dirty", [3]int{0, 1, 8}, true},
		{"sesh-v12.34.56", [3]int{12, 34, 56}, true},
		{"dev", [3]int{}, false},
		{"1a2b3c4", [3]int{}, false},
		{"", [3]int{}, false},
		{"0.1", [3]int{}, false},
		{"sesh-v0.1.8extra", [3]int{}, false},
	}
	for _, c := range cases {
		got, ok := parseVersion(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSupportWindowFloor(t *testing.T) {
	if f, ok := supportWindowFloor("sesh-v0.1.8"); !ok || f != [3]int{0, 1, 7} {
		t.Errorf("floor(sesh-v0.1.8) = %v, %v; want previous patch release", f, ok)
	}
	// Patch zero: the previous release's number is not derivable across a
	// minor bump, so the window narrows to current alone.
	if f, ok := supportWindowFloor("sesh-v0.2.0"); !ok || f != [3]int{0, 2, 0} {
		t.Errorf("floor(sesh-v0.2.0) = %v, %v; want current itself", f, ok)
	}
	// A dev/untagged store cannot pin a window; nobody gets flagged.
	if _, ok := supportWindowFloor("dev"); ok {
		t.Error("floor(dev) must be unknown")
	}
}

func TestCompareVersionsOrdering(t *testing.T) {
	if compareVersions([3]int{0, 1, 7}, [3]int{0, 1, 8}) >= 0 ||
		compareVersions([3]int{0, 1, 9}, [3]int{0, 1, 8}) <= 0 ||
		compareVersions([3]int{0, 2, 0}, [3]int{0, 1, 9}) <= 0 ||
		compareVersions([3]int{1, 0, 0}, [3]int{0, 9, 9}) <= 0 ||
		compareVersions([3]int{0, 1, 8}, [3]int{0, 1, 8}) != 0 {
		t.Error("compareVersions ordering broken")
	}
}
