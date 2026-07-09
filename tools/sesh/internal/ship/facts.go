package ship

import (
	"os"
	"os/user"
)

// Facts are the always-present node observations every PUT carries: hostname
// (the floor — "this node had this session") and the shipper's OS user (spec
// §3.2). The third shipper-side fact, SESSION_OWNER, is correlated per
// session (spec §4.2) and lives on the cursor once observed.
type Facts struct {
	Hostname string
	OSUser   string
}

// GatherFacts resolves the node facts once at startup.
func GatherFacts() (Facts, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return Facts{}, err
	}
	u, err := user.Current()
	if err != nil {
		return Facts{}, err
	}
	return Facts{Hostname: hostname, OSUser: u.Username}, nil
}
