//go:build darwin

package ship

// PlatformCorrelator returns nil on darwin: the shipper is facts-only there
// (spec §4.2, S11). No correlation is attempted, nothing references /proc,
// and its absence is not an error — tailnet identity is the better
// attribution signal on personal devices anyway.
func PlatformCorrelator() func([]Discovered) map[string]string {
	return nil
}
