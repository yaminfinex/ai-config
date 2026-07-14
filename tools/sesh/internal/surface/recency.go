package surface

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sesh/internal/wire"
)

// recencyPageLimit bounds every sessions-list render to the newest N logical
// sessions, cut at the query level by the Store (never by truncating a full
// listing here). Older history stays reachable through ?page=N links; with
// fleet corpora of thousands of files per node, one unbounded render is one
// browser tab OOM.
const recencyPageLimit = 50

// recencyPage is the template model for the sessions list (R14): ONE flat
// recency-ordered table — node and person are columns, never groupings
// (owner ruling 2026-07-14: group headers made page cuts fall mid-group) —
// bounded to one page, optionally filtered to one node.
type recencyPage struct {
	Now time.Time
	// PollSeconds drives the htmx refresh; pinned to the wire rescan
	// interval so the page keeps up with shipping without hammering.
	PollSeconds int
	Sessions    []sessionItem

	// Node is the active node-filter label (os_user@hostname); empty on the
	// all-nodes list. Every pager and poll URL on the page carries it.
	Node string

	// Paging facts: sessions From–To of Total are on this page, most recent
	// first. Page is 1-based.
	Total    int
	From, To int
	Page     int
	// FragmentURL is the htmx poll target for THIS page and filter, so a
	// periodic refresh never yanks a reader back to page one or drops the
	// node filter.
	FragmentURL string
	// NewerURL and OlderURL are the pager links; empty at the edges.
	NewerURL string
	OlderURL string
}

type sessionItem struct {
	SessionSummary
	// At is the effective R14 recency instant used for ordering.
	At               time.Time
	FullyQuarantined bool
	// Owner is the view-time attribution verdict (owner.go); it fills the
	// person column — honest absence renders as absence, conflicts badge.
	Owner DisplayOwner
	URL   string
	// Node is the row's os_user@hostname label; NodeURL filters the sessions
	// list to it.
	Node    string
	NodeURL string
}

// nodeLabel is the one display/filter spelling of a node identity.
func nodeLabel(hostname, osUser string) string {
	return osUser + "@" + hostname
}

// splitNodeLabel inverts nodeLabel. The OS user cannot contain '@' on any
// platform we ship from, so the FIRST '@' splits; a label without one
// matches no node and yields an honest empty list.
func splitNodeLabel(label string) (hostname, osUser string) {
	user, host, ok := strings.Cut(label, "@")
	if !ok {
		return label, ""
	}
	return host, user
}

// pageParam reads the 1-based ?page= selector; anything absent or malformed
// is page one.
func pageParam(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// sessionsURL builds a sessions-list URL (page or fragment path) carrying
// the node filter and page selector, omitting defaults so page one of the
// all-nodes list stays the bare stable URL.
func sessionsURL(path, node string, page int) string {
	q := url.Values{}
	if node != "" {
		q.Set("node", node)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

// maxRecencyPage caps the page selector so the offset arithmetic below (and
// the pager's page+1) can never overflow, whatever absurd-but-parseable
// value the query string carries.
const maxRecencyPage = (math.MaxInt - recencyPageLimit) / recencyPageLimit

func (s *Server) recencyData(ctx context.Context, page int, node string) (recencyPage, error) {
	if page < 1 {
		page = 1
	}
	if page > maxRecencyPage {
		page = maxRecencyPage
	}
	offset := (page - 1) * recencyPageLimit
	var (
		sums  []SessionSummary
		total int
		err   error
	)
	if node == "" {
		sums, total, err = s.store.RecentSessions(ctx, recencyPageLimit, offset)
	} else {
		hostname, osUser := splitNodeLabel(node)
		sums, total, err = s.store.RecentSessionsByNode(ctx, hostname, osUser, recencyPageLimit, offset)
	}
	if err != nil {
		return recencyPage{}, err
	}
	data := recencyPage{
		Now:         s.now(),
		PollSeconds: int(wire.RescanInterval / time.Second),
		Sessions:    sessionItems(sums),
		Node:        node,
		Total:       total,
		Page:        page,
		FragmentURL: sessionsURL("/fragments/recency", node, page),
	}
	if len(sums) > 0 {
		data.From = offset + 1
		data.To = offset + len(sums)
	}
	if page > 1 {
		// A newer link past the last real page points at the last real page,
		// not another empty one.
		newer := page - 1
		if lastPage := (total + recencyPageLimit - 1) / recencyPageLimit; lastPage > 0 && newer > lastPage {
			newer = lastPage
		}
		data.NewerURL = sessionsURL("/sessions", node, newer)
	}
	if offset+len(sums) < total {
		data.OlderURL = sessionsURL("/sessions", node, page+1)
	}
	return data, nil
}

// sessionItems builds the flat table rows. The Store returns the page
// already in recency order; this only decorates it.
func sessionItems(sums []SessionSummary) []sessionItem {
	items := make([]sessionItem, 0, len(sums))
	for _, sum := range sums {
		label := nodeLabel(sum.Hostname, sum.OSUser)
		items = append(items, sessionItem{
			SessionSummary:   sum,
			At:               sum.Recency(),
			FullyQuarantined: sum.FullyQuarantined(),
			Owner:            sum.DisplayOwner(),
			URL:              "/s/" + url.PathEscape(string(sum.Tool)) + "/" + url.PathEscape(sum.LogicalSessionID),
			Node:             label,
			NodeURL:          sessionsURL("/sessions", label, 1),
		})
	}
	return items
}
