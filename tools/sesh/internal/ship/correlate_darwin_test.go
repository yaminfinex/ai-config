//go:build darwin

package ship

// S11: darwin is facts-only — the platform correlator does not exist, and
// its absence is silent (the shipper treats a nil Correlate as "no
// correlation on this platform", never as an error). This file type-checks
// in the darwin cross-compile gate and runs on a real mac.

import "testing"

func TestDarwinHasNoCorrelator(t *testing.T) {
	if PlatformCorrelator() != nil {
		t.Fatal("darwin must ship facts-only: PlatformCorrelator() must be nil (S11)")
	}
}
