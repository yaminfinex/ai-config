package surface

import (
	"context"
	"net/url"
	"sort"
	"time"

	"sesh/internal/wire"
)

// recencyPage is the template model for the one page (R14): person → nodes →
// sessions, most recent first.
type recencyPage struct {
	Now time.Time
	// PollSeconds drives the htmx refresh; pinned to the wire rescan
	// interval so the page keeps up with shipping without hammering.
	PollSeconds int
	People      []personGroup
}

type personGroup struct {
	// Label is the display owner when a fact claims one, else the honest
	// node/OS-user identity. Source names the winning fact's origin; for
	// unclaimed sessions it says so instead of guessing (I1).
	Label   string
	Source  string
	Recency time.Time
	Nodes   []nodeGroup
}

type nodeGroup struct {
	Hostname string
	OSUser   string
	Recency  time.Time
	Sessions []sessionItem
}

type sessionItem struct {
	SessionSummary
	// At is the effective R14 recency instant used for ordering.
	At               time.Time
	FullyQuarantined bool
	// Owner is the view-time attribution verdict (owner.go); conflicts
	// badge on the row while the session groups under its node.
	Owner DisplayOwner
	URL   string
}

func (s *Server) recencyData(ctx context.Context) (recencyPage, error) {
	sums, err := s.store.Sessions(ctx)
	if err != nil {
		return recencyPage{}, err
	}
	return recencyPage{
		Now:         s.now(),
		PollSeconds: int(wire.RescanInterval / time.Second),
		People:      groupPeople(sums),
	}, nil
}

// groupPeople builds person → nodes → sessions, each level ordered most
// recent first with deterministic tie-breaks so renders are stable.
func groupPeople(sums []SessionSummary) []personGroup {
	type personKey struct{ label, source string }
	people := map[personKey]map[string][]sessionItem{} // person → node key → sessions

	nodeKey := func(sum SessionSummary) string { return sum.OSUser + "@" + sum.Hostname }
	for _, sum := range sums {
		owner := sum.DisplayOwner()
		item := sessionItem{
			SessionSummary:   sum,
			At:               sum.Recency(),
			FullyQuarantined: sum.FullyQuarantined(),
			Owner:            owner,
			URL:              "/s/" + url.PathEscape(string(sum.Tool)) + "/" + url.PathEscape(sum.LogicalSessionID),
		}
		key := personKey{owner.Name, owner.Source}
		if !owner.Claimed {
			// No identity claim won (unclaimed, node-tier, or conflicting):
			// the "person" is the node identity, honestly labeled as such —
			// never a guessed name. Conflicts badge on the session row.
			key = personKey{nodeKey(sum), "no owner claim — OS user @ host"}
		}
		if people[key] == nil {
			people[key] = map[string][]sessionItem{}
		}
		people[key][nodeKey(sum)] = append(people[key][nodeKey(sum)], item)
	}

	out := make([]personGroup, 0, len(people))
	for key, nodes := range people {
		person := personGroup{Label: key.label, Source: key.source}
		for _, items := range nodes {
			sort.Slice(items, func(i, j int) bool {
				if !items[i].At.Equal(items[j].At) {
					return items[i].At.After(items[j].At)
				}
				return items[i].LogicalSessionID < items[j].LogicalSessionID
			})
			node := nodeGroup{
				Hostname: items[0].Hostname,
				OSUser:   items[0].OSUser,
				Recency:  items[0].At,
				Sessions: items,
			}
			person.Nodes = append(person.Nodes, node)
		}
		sort.Slice(person.Nodes, func(i, j int) bool {
			a, b := person.Nodes[i], person.Nodes[j]
			if !a.Recency.Equal(b.Recency) {
				return a.Recency.After(b.Recency)
			}
			return a.OSUser+"@"+a.Hostname < b.OSUser+"@"+b.Hostname
		})
		person.Recency = person.Nodes[0].Recency
		out = append(out, person)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Recency.Equal(out[j].Recency) {
			return out[i].Recency.After(out[j].Recency)
		}
		return out[i].Label < out[j].Label
	})
	return out
}
