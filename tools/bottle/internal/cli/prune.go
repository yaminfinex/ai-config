package cli

import (
	"fmt"
	"sort"
)

// cmdPrune drops decants-map entries whose underlying harness session files
// are gone, and reports the count. Whether a session still exists is decided
// by the injected sessionExists seam (production stand-in: defaultSessionExists,
// pending U5's real locator). A decant whose bottle has itself been removed is
// orphaned and pruned unconditionally.
func cmdPrune(d *deps, args []string) int {
	decants, err := d.store.Decants()
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle prune: %v\n", err)
		return 1
	}
	var dead []string
	for sid, dec := range decants {
		b, err := d.store.Get(dec.BottleID)
		if err != nil {
			// Bottle gone — the decant entry is orphaned; drop it.
			dead = append(dead, sid)
			continue
		}
		if !d.sessionExists(&b.Meta, sid) {
			dead = append(dead, sid)
		}
	}
	sort.Strings(dead)
	if len(dead) > 0 {
		if err := d.store.RemoveDecants(dead...); err != nil {
			fmt.Fprintf(d.stderr, "bottle prune: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(d.stdout, "Pruned %d dead decant(s).\n", len(dead))
	return 0
}
