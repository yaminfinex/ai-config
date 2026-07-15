package main

import (
	"fmt"
	"html"
	"html/template"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var graphWindows = map[string]time.Duration{
	"10m": 10 * time.Minute,
	"30m": 30 * time.Minute,
	"2h":  2 * time.Hour,
	"8h":  8 * time.Hour,
}

type graphThreadInfo struct {
	Grade string
	Title string
}

type graphNode struct {
	Name, Kind, Mission, Tool, Status, Detail, Role, Branch, GUID string
	Unread, Activity                                              int
	X, Y, W, H                                                    float64
}

type graphEdge struct {
	A, B                  string
	AB, BA, Presence      int
	Newest                time.Time
	Managed, Raise        bool
	Threads, ThreadTitles []string
	HumanSenders          []string
}

func (e graphEdge) weight() int { return e.AB + e.BA + e.Presence }

type graphModel struct {
	AsOf     time.Time
	Window   string
	Nodes    []graphNode
	Edges    []graphEdge
	Warnings []string
	YourTurn int
}

type graphCacheEntry struct {
	at    time.Time
	model *graphModel
}

type graphCache struct {
	mu      sync.Mutex
	entries map[string]graphCacheEntry
}

type graphPage struct {
	Window, View, Mission, Focus string
	AsOf                         string
	Warning                      string
	AutoOffURL, Auto10URL        string
	Content                      template.HTML
	WindowLinks, ViewLinks       []graphNavLink
}

type graphNavLink struct {
	Label, URL string
	On         bool
}

// graphData builds the shared model used by both presentations. The cache is
// intentionally short: it collapses click bursts without making a live view
// look fresher than its bus snapshot.
func (w *Web) graphData(window string, now time.Time) *graphModel {
	w.graphCache.mu.Lock()
	defer w.graphCache.mu.Unlock()
	if hit, ok := w.graphCache.entries[window]; ok && now.Sub(hit.at) >= 0 && now.Sub(hit.at) < 3*time.Second {
		return hit.model
	}

	model := w.buildGraphModel(window, now)
	w.graphCache.entries[window] = graphCacheEntry{at: now, model: model}
	return model
}

func (w *Web) buildGraphModel(window string, now time.Time) *graphModel {
	m := &graphModel{AsOf: now, Window: window}
	groups, rosterWarning := w.rosterGroups(false)
	if rosterWarning != "" {
		m.Warnings = append(m.Warnings, "roster: "+rosterWarning)
	}

	nodes := map[string]*graphNode{}
	mentionNames := []string{w.seat}
	for _, group := range groups {
		mission := group.Mission
		if mission == "" {
			mission = "(no mission)"
		}
		// Bus events identify agents by their canonical bus name, so nodes
		// and mention fan-out must key on Address (falling back to Name for
		// bus-only rows where they coincide) or edges would never match.
		addAgent := func(agent rosterAgent) {
			name := cleanGraphName(agent.Address)
			if name == "" {
				name = cleanGraphName(agent.Name)
			}
			if name == "" {
				return
			}
			if !contains(mentionNames, name) {
				mentionNames = append(mentionNames, name)
			}
			if name == w.seat {
				return
			}
			nodes[name] = &graphNode{
				Name: name, Kind: "agent", Mission: mission, Tool: agent.Tool,
				Status: agent.Status, Detail: agent.Detail, Unread: agent.Unread,
				Role: agent.Role, Branch: agent.Branch, GUID: agent.GUID,
			}
		}
		for _, agent := range group.Agents {
			addAgent(agent)
		}
		// Missionless agents sit nested repo → branch, not in the flat list.
		for _, repo := range group.Repos {
			for _, branch := range repo.Branches {
				for _, agent := range branch.Agents {
					addAgent(agent)
				}
			}
		}
	}
	nodes[w.seat] = &graphNode{Name: w.seat, Kind: "desk", Status: "active"}

	authors, threadInfo, yourTurn := w.store.GraphSnapshot()
	m.YourTurn = yourTurn
	events, busWarning := w.bus.RecentMessages(now.Add(-graphWindows[window]), 500, mentionNames...)
	if busWarning != nil {
		m.Warnings = append(m.Warnings, "recent bus traffic: "+busWarning.Error())
	}

	windowAuthors := make(map[int64]string, len(events))
	threadParticipants := map[string]map[string]time.Time{}
	edges := map[string]*graphEdge{}
	ensureNode := func(name string) {
		if name == "" || nodes[name] != nil {
			return
		}
		nodes[name] = &graphNode{Name: name, Kind: "ghost", Mission: "(no mission)", Status: "gone"}
	}
	for _, ev := range events {
		from := w.graphIdentity(ev.Data.From)
		windowAuthors[ev.ID] = from
		if ev.Data.Thread != "" && from != "" {
			if threadParticipants[ev.Data.Thread] == nil {
				threadParticipants[ev.Data.Thread] = map[string]time.Time{}
			}
			threadParticipants[ev.Data.Thread][from] = laterTime(threadParticipants[ev.Data.Thread][from], graphEventTime(ev.TS, now))
		}
	}

	addDirected := func(from, to string, ev BusEvent, raise bool) {
		if from == "" || to == "" || from == to {
			return
		}
		ensureNode(from)
		ensureNode(to)
		a, b := from, to
		forward := true
		if b < a {
			a, b, forward = b, a, false
		}
		key := a + "\x00" + b
		edge := edges[key]
		if edge == nil {
			edge = &graphEdge{A: a, B: b}
			edges[key] = edge
		}
		if forward {
			edge.AB++
		} else {
			edge.BA++
		}
		edge.Newest = laterTime(edge.Newest, graphEventTime(ev.TS, now))
		edge.Raise = edge.Raise || raise
		decorateGraphEdge(edge, ev.Data.Thread, threadInfo)
		if strings.HasPrefix(ev.Data.From, "human-") && !contains(edge.HumanSenders, ev.Data.From) {
			edge.HumanSenders = append(edge.HumanSenders, ev.Data.From)
		}
	}

	for _, ev := range events {
		from := w.graphIdentity(ev.Data.From)
		for _, rawTarget := range ev.Data.Mentions {
			targetName := cleanGraphName(rawTarget)
			if targetName == "bigboss" { // implicit-stamp trap: never an edge
				continue
			}
			target := w.graphIdentity(targetName)
			addDirected(from, target, ev, target == w.seat)
		}
		if ev.Data.ReplyTo > 0 {
			target := windowAuthors[ev.Data.ReplyTo]
			if target == "" {
				target = w.graphIdentity(authors[ev.Data.ReplyTo])
			}
			addDirected(from, target, ev, false)
		}
	}

	// Thread co-presence is one undirected contribution per pair per thread,
	// regardless of message volume.
	for thread, participants := range threadParticipants {
		var names []string
		for name := range participants {
			names = append(names, name)
		}
		sort.Strings(names)
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				a, b := names[i], names[j]
				if a == b {
					continue
				}
				ensureNode(a)
				ensureNode(b)
				key := a + "\x00" + b
				edge := edges[key]
				if edge == nil {
					edge = &graphEdge{A: a, B: b}
					edges[key] = edge
				}
				edge.Presence++
				edge.Newest = laterTime(edge.Newest, laterTime(participants[a], participants[b]))
				decorateGraphEdge(edge, thread, threadInfo)
			}
		}
	}

	for _, edge := range edges {
		nodes[edge.A].Activity += edge.weight()
		nodes[edge.B].Activity += edge.weight()
		m.Edges = append(m.Edges, *edge)
	}
	for _, node := range nodes {
		m.Nodes = append(m.Nodes, *node)
	}
	sort.Slice(m.Nodes, func(i, j int) bool {
		if m.Nodes[i].Mission != m.Nodes[j].Mission {
			return m.Nodes[i].Mission < m.Nodes[j].Mission
		}
		if m.Nodes[i].Activity != m.Nodes[j].Activity {
			return m.Nodes[i].Activity > m.Nodes[j].Activity
		}
		return m.Nodes[i].Name < m.Nodes[j].Name
	})
	sort.Slice(m.Edges, func(i, j int) bool {
		if m.Edges[i].A != m.Edges[j].A {
			return m.Edges[i].A < m.Edges[j].A
		}
		return m.Edges[i].B < m.Edges[j].B
	})
	return m
}

func decorateGraphEdge(edge *graphEdge, thread string, info map[string]graphThreadInfo) {
	if thread == "" {
		return
	}
	if !contains(edge.Threads, thread) {
		edge.Threads = append(edge.Threads, thread)
		sort.Strings(edge.Threads)
	}
	threadDetails := info[thread]
	if threadDetails.Title != "" && !contains(edge.ThreadTitles, threadDetails.Title) {
		edge.ThreadTitles = append(edge.ThreadTitles, threadDetails.Title)
		sort.Strings(edge.ThreadTitles)
	}
	if threadDetails.Grade == "managed" {
		edge.Managed = true
	}
}

func (w *Web) graphIdentity(name string) string {
	name = cleanGraphName(name)
	if strings.HasPrefix(name, "human-") || name == w.seat {
		return w.seat
	}
	return name
}

func cleanGraphName(name string) string { return strings.TrimPrefix(strings.TrimSpace(name), "@") }

func graphEventTime(raw string, fallback time.Time) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed
		}
	}
	return fallback
}

func laterTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func graphSubset(model *graphModel, mission string) ([]graphNode, []graphEdge) {
	var nodes []graphNode
	keep := map[string]bool{}
	for _, node := range model.Nodes {
		if mission != "" && node.Kind != "desk" && node.Mission != mission {
			continue
		}
		nodes = append(nodes, node)
		keep[node.Name] = true
	}
	var edges []graphEdge
	for _, edge := range model.Edges {
		if keep[edge.A] && keep[edge.B] {
			edges = append(edges, edge)
		}
	}
	return nodes, edges
}

func renderGraphSVG(model *graphModel, mission, focus, user string, query url.Values) template.HTML {
	nodes, edges := graphSubset(model, mission)
	height := layoutGraph(nodes)
	byName := map[string]graphNode{}
	for _, node := range nodes {
		byName[node.Name] = node
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="mc-graph" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 960 %.0f" role="img" aria-labelledby="graph-title graph-desc">`, height)
	b.WriteString(`<title id="graph-title">Live agent communication map</title><desc id="graph-desc">Agents clustered by mission with recent communication edges and a desk node for the owner seat.</desc>`)
	b.WriteString(`<style>:root{--fg:#1a1a1a;--dim:#777;--line:#d7d7d2;--accent:#0b57d0;--card:#fff;--cluster:#f6f6f2;--warn:#b3261e}.cluster{fill:var(--cluster);stroke:var(--line)}.cluster-label{font:600 13px system-ui,sans-serif;fill:var(--dim)}.node rect{fill:var(--card);stroke:var(--fg);stroke-width:1.5}.node text{font:13px system-ui,sans-serif;fill:var(--fg)}.node .meta{font-size:11px;fill:var(--dim)}.node .unread-text{fill:#fff}.node.ghost{opacity:.42}.node.ghost rect{fill:none;stroke-dasharray:4 3}.node.desk rect{stroke:var(--accent);stroke-width:2}.edge{fill:none;stroke:var(--dim);stroke-dasharray:6 5}.edge.managed{stroke:var(--accent);stroke-dasharray:none}.edge.raise{stroke:var(--warn)}.edge.cool2{opacity:.6}.edge.cool3{opacity:.3}.edge-label{font:11px system-ui,sans-serif;fill:var(--dim);paint-order:stroke;stroke:var(--card);stroke-width:3px}.dim{opacity:.12}.legend{font:12px system-ui,sans-serif;fill:var(--dim)}a:focus .node rect{stroke-width:3}</style>`)
	b.WriteString(`<defs><marker id="arrow-dim" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto-start-reverse" markerUnits="strokeWidth"><path d="M0,0 L8,4 L0,8 z" fill="#777"/></marker><marker id="arrow-accent" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto-start-reverse" markerUnits="strokeWidth"><path d="M0,0 L8,4 L0,8 z" fill="#0b57d0"/></marker><marker id="arrow-raise" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto-start-reverse" markerUnits="strokeWidth"><path d="M0,0 L8,4 L0,8 z" fill="#b3261e"/></marker></defs>`)

	clusters := graphClusters(nodes)
	for _, cluster := range clusters {
		fmt.Fprintf(&b, `<rect class="cluster" x="%.0f" y="%.0f" width="%.0f" height="%.0f" rx="10"/><text class="cluster-label" x="%.0f" y="%.0f">mission: %s</text>`, cluster.x, cluster.y, cluster.w, cluster.h, cluster.x+14, cluster.y+21, html.EscapeString(cluster.name))
	}

	now := model.AsOf
	for _, edge := range edges {
		a, aok := byName[edge.A]
		z, zok := byName[edge.B]
		if !aok || !zok {
			continue
		}
		x1, y1, x2, y2 := graphEdgeEndpoints(a, z)
		class := "edge "
		if edge.Managed {
			class += "managed "
		} else {
			class += "observed "
		}
		if edge.Raise {
			class += "raise "
		}
		age := now.Sub(edge.Newest)
		if age > 15*time.Minute {
			class += "cool3 "
		} else if age > 5*time.Minute {
			class += "cool2 "
		}
		if focus != "" && edge.A != focus && edge.B != focus {
			class += "dim "
		}
		width := math.Min(4, 1+math.Log2(float64(max(1, edge.weight()))))
		marker := ""
		if edge.AB > 0 && edge.BA == 0 {
			marker = markerFor(edge, false)
		} else if edge.BA > 0 && edge.AB == 0 {
			x1, x2, y1, y2 = x2, x1, y2, y1
			marker = markerFor(edge, false)
		} else if edge.AB > 0 && edge.BA > 0 {
			marker = markerFor(edge, true)
		}
		title := graphEdgeTitle(edge)
		fmt.Fprintf(&b, `<g class="%s"><title>%s</title><line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke-width="%.1f"%s/>`, html.EscapeString(strings.TrimSpace(class)), html.EscapeString(title), x1, y1, x2, y2, width, marker)
		if edge.weight() >= 3 {
			fmt.Fprintf(&b, `<text class="edge-label" x="%.1f" y="%.1f" text-anchor="middle">%d%s</text>`, (x1+x2)/2, (y1+y2)/2-5, edge.weight(), map[bool]string{true: "·raise"}[edge.Raise])
		}
		b.WriteString(`</g>`)
	}

	for _, node := range nodes {
		class := "node " + node.Kind
		if focus != "" && node.Name != focus {
			class += " dim"
		}
		link := graphNodeURL(node, focus, query)
		fmt.Fprintf(&b, `<a href="%s"><g class="%s"><title>%s</title><rect x="%.0f" y="%.0f" width="%.0f" height="%.0f" rx="8"/>`, html.EscapeString(link), html.EscapeString(class), html.EscapeString(graphNodeTitle(node)), node.X, node.Y, node.W, node.H)
		label := node.Name
		if node.Kind == "desk" {
			label = "desk @" + node.Name
		}
		fmt.Fprintf(&b, `<text x="%.0f" y="%.0f">%s %s</text>`, node.X+12, node.Y+22, graphStatusGlyph(node), html.EscapeString(label))
		if node.Kind == "desk" {
			fmt.Fprintf(&b, `<text class="meta" x="%.0f" y="%.0f">%s · your turn: %d</text>`, node.X+12, node.Y+42, html.EscapeString(user), model.YourTurn)
		} else {
			meta := strings.TrimSpace(strings.Join(nonempty(node.Tool, node.Status), " · "))
			fmt.Fprintf(&b, `<text class="meta" x="%.0f" y="%.0f">%s</text>`, node.X+12, node.Y+42, html.EscapeString(meta))
			if node.Unread > 0 {
				fmt.Fprintf(&b, `<circle cx="%.0f" cy="%.0f" r="11" fill="#0b57d0"/><text class="unread-text" x="%.0f" y="%.0f" text-anchor="middle" font-size="10">%d</text>`, node.X+node.W-15, node.Y+15, node.X+node.W-15, node.Y+19, node.Unread)
			}
		}
		b.WriteString(`</g></a>`)
	}
	fmt.Fprintf(&b, `<text class="legend" x="24" y="%.0f">● listening · ◉ active · ▲! blocked · ○ ghost · solid managed · dashed observed · arrow direction · width messages · faded cooling</text>`, height-18)
	b.WriteString(`</svg>`)
	return template.HTML(b.String()) // all model- and URL-derived strings escaped above
}

type graphCluster struct {
	name       string
	x, y, w, h float64
}

func layoutGraph(nodes []graphNode) float64 {
	groups := map[string][]int{}
	var desk []int
	for i := range nodes {
		if nodes[i].Kind == "desk" {
			desk = append(desk, i)
			continue
		}
		groups[nodes[i].Mission] = append(groups[nodes[i].Mission], i)
	}
	var names []string
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	columnY := [2]float64{24, 24}
	for i, name := range names {
		col := i % 2
		x, y := 24+float64(col)*456, columnY[col]
		idxs := groups[name]
		sort.Slice(idxs, func(i, j int) bool {
			a, b := nodes[idxs[i]], nodes[idxs[j]]
			if a.Activity != b.Activity {
				return a.Activity > b.Activity
			}
			return a.Name < b.Name
		})
		rows := (len(idxs) + 1) / 2
		h := 46 + float64(rows)*72
		for j, idx := range idxs {
			nodes[idx].X = x + 14 + float64(j%2)*207
			nodes[idx].Y = y + 32 + float64(j/2)*72
			nodes[idx].W, nodes[idx].H = 190, 54
		}
		columnY[col] += h + 18
	}
	bottom := math.Max(columnY[0], columnY[1]) + 18
	for _, idx := range desk {
		nodes[idx].X, nodes[idx].Y, nodes[idx].W, nodes[idx].H = 355, bottom, 250, 60
	}
	return bottom + 106
}

func graphClusters(nodes []graphNode) []graphCluster {
	byMission := map[string]graphCluster{}
	for _, node := range nodes {
		if node.Kind == "desk" {
			continue
		}
		c, ok := byMission[node.Mission]
		if !ok {
			byMission[node.Mission] = graphCluster{name: node.Mission, x: node.X - 14, y: node.Y - 32, w: 426, h: 100}
			continue
		}
		bottom := node.Y + node.H + 14
		if bottom-c.y > c.h {
			c.h = bottom - c.y
		}
		byMission[node.Mission] = c
	}
	var out []graphCluster
	for _, cluster := range byMission {
		out = append(out, cluster)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].y != out[j].y {
			return out[i].y < out[j].y
		}
		return out[i].x < out[j].x
	})
	return out
}

func graphEdgeEndpoints(from, to graphNode) (float64, float64, float64, float64) {
	fx, fy := from.X+from.W/2, from.Y+from.H/2
	tx, ty := to.X+to.W/2, to.Y+to.H/2
	dx, dy := tx-fx, ty-fy
	if dx == 0 && dy == 0 {
		return fx, fy, tx, ty
	}
	borderScale := func(w, h float64) float64 {
		sx, sy := math.Inf(1), math.Inf(1)
		if dx != 0 {
			sx = (w / 2) / math.Abs(dx)
		}
		if dy != 0 {
			sy = (h / 2) / math.Abs(dy)
		}
		return math.Min(sx, sy)
	}
	fromScale := borderScale(from.W, from.H)
	toScale := borderScale(to.W, to.H)
	return fx + dx*fromScale, fy + dy*fromScale, tx - dx*toScale, ty - dy*toScale
}

func markerFor(edge graphEdge, both bool) string {
	id := "arrow-dim"
	if edge.Managed {
		id = "arrow-accent"
	}
	if edge.Raise {
		id = "arrow-raise"
	}
	attrs := ` marker-end="url(#` + id + `)"`
	if both {
		attrs += ` marker-start="url(#` + id + `)"`
	}
	return attrs
}

func graphStatusGlyph(node graphNode) string {
	if node.Kind == "ghost" {
		return "○"
	}
	switch node.Status {
	case "active":
		return "◉"
	case "blocked":
		return "▲!"
	default:
		return "●"
	}
}

func graphNodeTitle(node graphNode) string {
	parts := []string{node.Name, node.Tool, node.Status, node.Role, node.Branch, node.Detail}
	return strings.Join(nonempty(parts...), " · ")
}

func graphEdgeTitle(edge graphEdge) string {
	parts := []string{fmt.Sprintf("%s → %s: %d", edge.A, edge.B, edge.AB), fmt.Sprintf("%s → %s: %d", edge.B, edge.A, edge.BA)}
	if edge.Presence > 0 {
		parts = append(parts, fmt.Sprintf("thread co-presence: %d", edge.Presence))
	}
	if len(edge.Threads) > 0 {
		parts = append(parts, "threads: "+strings.Join(edge.Threads, ", "))
	}
	if len(edge.ThreadTitles) > 0 {
		parts = append(parts, "topics: "+strings.Join(edge.ThreadTitles, ", "))
	}
	if len(edge.HumanSenders) > 0 {
		parts = append(parts, "desk driven by "+strings.Join(edge.HumanSenders, ", "))
	}
	if edge.Raise {
		parts = append(parts, "raise")
	}
	return strings.Join(parts, " · ")
}

func graphNodeURL(node graphNode, focus string, query url.Values) string {
	if focus == node.Name {
		return "/talk?agent=" + url.QueryEscape(node.Name)
	}
	q := cloneValues(query)
	q.Set("focus", node.Name)
	return "/graph?" + q.Encode()
}

func renderGraphMatrix(model *graphModel, mission, focus string) template.HTML {
	nodes, edges := graphSubset(model, mission)
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Activity != nodes[j].Activity {
			return nodes[i].Activity > nodes[j].Activity
		}
		return nodes[i].Name < nodes[j].Name
	})
	byPair := map[string]graphEdge{}
	for _, edge := range edges {
		byPair[edge.A+"\x00"+edge.B] = edge
	}
	var b strings.Builder
	b.WriteString(`<div class="graph-matrix"><table><caption>Recent communication counts; rows are senders, columns are recipients.</caption><thead><tr><th scope="col">from ↓ / to →</th>`)
	for _, node := range nodes {
		fmt.Fprintf(&b, `<th scope="col">%s</th>`, html.EscapeString(graphMatrixName(node)))
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, from := range nodes {
		fmt.Fprintf(&b, `<tr><th scope="row">%s</th>`, html.EscapeString(graphMatrixName(from)))
		for _, to := range nodes {
			if from.Name == to.Name {
				b.WriteString(`<td class="graph-empty">·</td>`)
				continue
			}
			a, z := from.Name, to.Name
			forward := true
			if z < a {
				a, z, forward = z, a, false
			}
			edge, ok := byPair[a+"\x00"+z]
			if !ok {
				b.WriteString(`<td class="graph-empty">·</td>`)
				continue
			}
			count := edge.AB + edge.Presence
			if !forward {
				count = edge.BA + edge.Presence
			}
			if count == 0 {
				b.WriteString(`<td class="graph-empty">·</td>`)
				continue
			}
			class := "graph-cell observed"
			if edge.Managed {
				class = "graph-cell managed"
			}
			if edge.Raise && to.Kind == "desk" {
				class += " raise"
			}
			age := model.AsOf.Sub(edge.Newest)
			if age > 15*time.Minute {
				class += " cool3"
			} else if age > 5*time.Minute {
				class += " cool2"
			}
			if focus != "" && from.Name != focus && to.Name != focus {
				class += " graph-dim"
			}
			label := strconv.Itoa(count)
			if len(edge.Threads) > 0 {
				label = `<a href="/thread/` + url.PathEscape(edge.Threads[0]) + `" title="` + html.EscapeString(graphEdgeTitle(edge)) + `">` + label + `</a>`
			}
			fmt.Fprintf(&b, `<td class="%s">%s</td>`, class, label)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table><p class="meta">Shading follows recency; bold cells are managed; dashed cells are observed. Counts include directed signals and one co-presence contribution per thread pair.</p></div>`)
	return template.HTML(b.String()) // all model-derived strings escaped above
}

func graphMatrixName(node graphNode) string {
	if node.Kind == "desk" {
		return "@" + node.Name
	}
	if node.Kind == "ghost" {
		return node.Name + " ○"
	}
	return node.Name
}

func graphPageLinks(q url.Values, window, view string) ([]graphNavLink, []graphNavLink) {
	var windows []graphNavLink
	for _, choice := range []string{"10m", "30m", "2h", "8h"} {
		v := cloneValues(q)
		v.Set("window", choice)
		windows = append(windows, graphNavLink{Label: choice, URL: "/graph?" + v.Encode(), On: window == choice})
	}
	var views []graphNavLink
	for _, choice := range []string{"map", "matrix"} {
		v := cloneValues(q)
		v.Set("view", choice)
		views = append(views, graphNavLink{Label: choice, URL: "/graph?" + v.Encode(), On: view == choice})
	}
	return windows, views
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func nonempty(values ...string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
