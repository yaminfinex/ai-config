package surface

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"sesh/internal/wire"
)

// Render caps. The mirror is the untruncated truth; the surface is a viewer
// and cuts pathological lines instead of shipping megabytes into a page
// (plan U7 test scenario: multi-MB single line truncates, raw stays
// available).
const (
	// maxParseLineBytes: index rows whose byte span exceeds this are not
	// JSON-parsed for display; they render as a truncated raw excerpt.
	maxParseLineBytes = 1 << 20
	// maxBlockChars caps any one rendered block's text.
	maxBlockChars = 16 << 10
	// excerptBytes is how much of an unparseable/oversized line is shown.
	excerptBytes = 4 << 10
	// transcriptDisplayBudgetBytes bounds the total block text one
	// transcript page renders. Pages render buffered, so many
	// individually-renderable large lines would otherwise accumulate
	// without bound; past the budget the page stops with an honest
	// omitted-rows notice and points at the raw view.
	transcriptDisplayBudgetBytes = 8 << 20
	// transcriptWindowMessages bounds one transcript page to a window of the
	// session's index rows, newest window first — the same pager idiom as
	// the sessions list. A session renders whole only when it fits one
	// window; older history stays reachable through ?page=N links, and the
	// raw route stays whole-file. The display budget above remains the
	// byte-level backstop within a window.
	transcriptWindowMessages = 200
)

// maxTranscriptPage caps the ?page= selector so the window arithmetic can
// never overflow (same posture as maxRecencyPage).
const maxTranscriptPage = (math.MaxInt - transcriptWindowMessages) / transcriptWindowMessages

// transcriptPage is the template model for drill-down (R16), one message
// window at a time.
type transcriptPage struct {
	Session SessionSummary
	RawURL  string
	Entries []displayEntry
	// OmittedRows counts index rows the display budget kept off the page.
	OmittedRows int
	// BranchNotice labels Pi's explicit degraded floor when a malformed tree
	// cannot be resolved safely. A valid Pi tree instead labels its branch
	// points on the active-path entries.
	BranchNotice string

	// Window pager: index rows From–To of Total (1-based, oldest-first
	// numbering) are on this page. Page 1 is the NEWEST window; OlderURL and
	// NewerURL walk the history and are empty at the edges.
	Total    int
	From, To int
	Page     int
	NewerURL string
	OlderURL string
}

// transcriptData windows the session's sorted index rows to one page and
// builds its display entries. Page numbers past the last real window clamp
// to the oldest window — the page stays honest about what it shows and the
// never-500 contract holds for any ?page= value.
func (s *Server) transcriptData(ctx context.Context, sum SessionSummary, rows []wire.IndexMessage, pageN int) transcriptPage {
	markers := map[transcriptRowKey]piBranchMarker{}
	branchNotice := ""
	if sum.Tool == wire.ToolPi {
		rows, markers, branchNotice = s.piActiveBranch(ctx, rows)
	}
	sortTranscript(rows)
	total := len(rows)
	lastPage := (total + transcriptWindowMessages - 1) / transcriptWindowMessages
	if lastPage < 1 {
		lastPage = 1
	}
	switch {
	case pageN < 1:
		pageN = 1
	case pageN > maxTranscriptPage:
		pageN = maxTranscriptPage
	}
	if pageN > lastPage {
		pageN = lastPage
	}
	end := total - (pageN-1)*transcriptWindowMessages
	start := end - transcriptWindowMessages
	if start < 0 {
		start = 0
	}
	page := transcriptPage{
		Session:      sum,
		RawURL:       s.rawURL(sum),
		Total:        total,
		From:         start + 1,
		To:           end,
		Page:         pageN,
		BranchNotice: branchNotice,
	}
	if pageN > 1 {
		page.NewerURL = transcriptPageURL(s.transcriptURL(sum), pageN-1)
	}
	if start > 0 {
		page.OlderURL = transcriptPageURL(s.transcriptURL(sum), pageN+1)
	}
	page.Entries, page.OmittedRows = s.buildEntries(ctx, sum.Tool, rows[start:end], markers)
	return page
}

func transcriptPageURL(base string, page int) string {
	if page <= 1 {
		return base
	}
	return base + "?page=" + strconv.Itoa(page)
}

// displayEntry renders one index row.
type displayEntry struct {
	Role             string
	EntryType        string
	MessageUUID      string
	Timestamp        *time.Time
	FileUUID         string
	Generation       int
	Quarantined      bool
	QuarantineReason string
	Truncated        bool
	ByteSize         int64
	Blocks           []displayBlock
	BranchPoint      string
	BranchLabel      string
}

// displayBlock is one piece of an entry's body. Kind steers styling: text,
// thinking, tool_use, tool_result, meta, raw. Collapsed blocks render inside
// <details> (tool calls collapsed, R16).
type displayBlock struct {
	Kind      string
	Title     string
	Text      string
	Collapsed bool
	Truncated bool
}

// sortTranscript applies the frozen surface ordering from the wire doc:
// (timestamp_utc nulls last, file_ordinal, line_ordinal, file_uuid,
// generation). Never parentUuid chains (R16).
func sortTranscript(rows []wire.IndexMessage) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch {
		case a.TimestampUTC == nil && b.TimestampUTC != nil:
			return false
		case a.TimestampUTC != nil && b.TimestampUTC == nil:
			return true
		case a.TimestampUTC != nil && b.TimestampUTC != nil && !a.TimestampUTC.Equal(*b.TimestampUTC):
			return a.TimestampUTC.Before(*b.TimestampUTC)
		}
		if a.FileOrdinal != b.FileOrdinal {
			return a.FileOrdinal < b.FileOrdinal
		}
		if a.LineOrdinal != b.LineOrdinal {
			return a.LineOrdinal < b.LineOrdinal
		}
		if a.FileUUID != b.FileUUID {
			return a.FileUUID < b.FileUUID
		}
		return a.Generation < b.Generation
	})
}

// buildEntries turns already-sorted index rows into display entries, reading
// each row's line bytes back from the mirror. Every step is defensive: a row
// that cannot be read or parsed renders as a raw excerpt, never an error
// page. It stops once rendered text exceeds the display budget and reports
// how many rows were left off the page.
func (s *Server) buildEntries(ctx context.Context, tool wire.Tool, rows []wire.IndexMessage, markers map[transcriptRowKey]piBranchMarker) (entries []displayEntry, omitted int) {
	// Mirror-range failures repeat per row when a whole generation is
	// unreadable, so they aggregate to one journal line per error class per
	// request (up to a window of them) instead of a line per row.
	mirrorFails := map[string]int{}
	defer func() {
		for class, n := range mirrorFails {
			s.log.Warn("surface: mirror range read failed", "tool", string(tool), "error_class", class, "rows", n)
		}
	}()
	entries = make([]displayEntry, 0, len(rows))
	var spent int64
	for i, row := range rows {
		if spent > transcriptDisplayBudgetBytes {
			return entries, len(rows) - i
		}
		entry := s.buildEntry(ctx, tool, row, mirrorFails)
		if marker, ok := markers[transcriptKey(row)]; ok {
			entry.BranchPoint = marker.Point
			entry.BranchLabel = marker.Label
		}
		for _, b := range entry.Blocks {
			spent += int64(len(b.Text))
		}
		entries = append(entries, entry)
	}
	return entries, 0
}

func (s *Server) buildEntry(ctx context.Context, tool wire.Tool, row wire.IndexMessage, mirrorFails map[string]int) displayEntry {
	entry := displayEntry{
		Role:             row.Role,
		EntryType:        row.EntryType,
		MessageUUID:      row.MessageUUID,
		Timestamp:        row.TimestampUTC,
		FileUUID:         row.FileUUID,
		Generation:       row.Generation,
		Quarantined:      row.Quarantine,
		QuarantineReason: row.QuarantineReason,
		ByteSize:         row.ByteEnd - row.ByteStart,
	}

	readEnd := row.ByteEnd
	if entry.ByteSize > maxParseLineBytes {
		entry.Truncated = true
		readEnd = row.ByteStart + excerptBytes
	}
	line, err := s.store.MirrorRange(ctx, tool, row.WireSessionID, row.FileUUID, row.Generation, row.ByteStart, readEnd)
	if err != nil {
		mirrorFails[errClass(err)]++
		entry.Blocks = []displayBlock{{
			Kind:      "raw",
			Title:     "mirror line unavailable",
			Text:      fmt.Sprintf("could not read mirrored bytes [%d, %d)", row.ByteStart, row.ByteEnd),
			Collapsed: false,
		}}
		return entry
	}
	line = bytes.TrimRight(line, "\r\n")

	switch {
	case entry.Truncated:
		entry.Blocks = []displayBlock{rawExcerptBlock(line, entry.ByteSize,
			fmt.Sprintf("oversized line truncated for display (%s) — full bytes in raw view", fmtSize(entry.ByteSize)))}
	case row.Quarantine:
		b := rawExcerptBlock(line, entry.ByteSize, "quarantined line")
		b.Collapsed = true
		entry.Blocks = []displayBlock{b}
	default:
		entry.Blocks = renderLine(tool, line, entry.ByteSize)
	}
	return entry
}

// rawExcerptBlock shows the head of a line verbatim (escaped by the
// template) with an honest size label.
func rawExcerptBlock(line []byte, totalBytes int64, title string) displayBlock {
	text, cut := truncateText(string(line), excerptBytes)
	return displayBlock{Kind: "raw", Title: title, Text: text, Truncated: cut || int64(len(line)) < totalBytes}
}

// renderLine parses one complete mirrored JSONL line for display. Format
// knowledge here is store-side render heuristics only — parse failures fall
// back to a raw excerpt; they never fail the page (format churn is expected,
// the raw fallback is the escape hatch).
func renderLine(tool wire.Tool, line []byte, totalBytes int64) []displayBlock {
	var blocks []displayBlock
	switch tool {
	case wire.ToolClaude:
		blocks = claudeBlocks(line)
	case wire.ToolCodex:
		blocks = codexBlocks(line)
	case wire.ToolGrok:
		blocks = grokBlocks(line)
	case wire.ToolPi:
		blocks = piBlocks(line)
	}
	if len(blocks) == 0 {
		b := rawExcerptBlock(line, totalBytes, "unrecognized entry")
		b.Collapsed = true
		blocks = []displayBlock{b}
	}
	return blocks
}

type transcriptRowKey struct {
	FileUUID   string
	Generation int
	Line       int64
}

func transcriptKey(row wire.IndexMessage) transcriptRowKey {
	return transcriptRowKey{FileUUID: row.FileUUID, Generation: row.Generation, Line: row.LineOrdinal}
}

type piBranchMarker struct {
	Point string
	Label string
}

type piTreeLine struct {
	Type     string  `json:"type"`
	ID       string  `json:"id"`
	ParentID *string `json:"parentId"`
	TargetID string  `json:"targetId"`
	Label    *string `json:"label"`
}

type piTreeNode struct {
	row    wire.IndexMessage
	parent string
}

// piActiveBranch resolves Pi's append-only id/parentId tree using mirrored
// line bytes. The last valid tree entry is Pi's active leaf (the vendor
// SessionManager uses the same rule). Only its root-to-leaf path renders;
// nodes with multiple children are explicitly labeled. The frozen index has
// no parent column, so this remains a read-side adapter and changes no DDL.
func (s *Server) piActiveBranch(ctx context.Context, rows []wire.IndexMessage) ([]wire.IndexMessage, map[transcriptRowKey]piBranchMarker, string) {
	nodes := map[string]piTreeNode{}
	children := map[string][]string{}
	labels := map[string]string{}
	headers := map[transcriptRowKey]bool{}
	leaf := ""
	malformed := false
	for _, row := range rows {
		if row.Quarantine || row.ByteEnd-row.ByteStart > maxParseLineBytes {
			malformed = true
			continue
		}
		line, err := s.store.MirrorRange(ctx, wire.ToolPi, row.WireSessionID, row.FileUUID, row.Generation, row.ByteStart, row.ByteEnd)
		if err != nil {
			malformed = true
			continue
		}
		var entry piTreeLine
		if json.Unmarshal(bytes.TrimRight(line, "\r\n"), &entry) != nil || entry.Type == "" {
			malformed = true
			continue
		}
		if entry.Type == "session" {
			headers[transcriptKey(row)] = true
			continue
		}
		if entry.ID == "" {
			malformed = true
			continue
		}
		if _, duplicate := nodes[entry.ID]; duplicate {
			malformed = true
			continue
		}
		parent := ""
		if entry.ParentID != nil {
			parent = *entry.ParentID
		}
		nodes[entry.ID] = piTreeNode{row: row, parent: parent}
		children[parent] = append(children[parent], entry.ID)
		leaf = entry.ID
		if entry.Type == "label" && entry.TargetID != "" {
			if entry.Label == nil || *entry.Label == "" {
				delete(labels, entry.TargetID)
			} else {
				labels[entry.TargetID] = *entry.Label
			}
		}
	}
	if len(rows) == 0 {
		return rows, map[transcriptRowKey]piBranchMarker{}, "Pi transcript is empty; no active branch is available."
	}
	if leaf == "" {
		return rows, map[transcriptRowKey]piBranchMarker{}, "Pi branch graph is unavailable; showing the bounded degraded file-order view."
	}
	active := map[string]bool{}
	activeChild := map[string]string{}
	for id, steps := leaf, 0; id != ""; steps++ {
		if steps > len(nodes) || active[id] {
			malformed = true
			break
		}
		node, ok := nodes[id]
		if !ok {
			malformed = true
			break
		}
		active[id] = true
		if node.parent != "" {
			activeChild[node.parent] = id
		}
		id = node.parent
	}
	if malformed {
		return rows, map[transcriptRowKey]piBranchMarker{}, "Pi branch graph is malformed or incomplete; showing the bounded degraded file-order view."
	}
	selected := make([]wire.IndexMessage, 0, len(active)+len(headers))
	markers := map[transcriptRowKey]piBranchMarker{}
	for _, row := range rows {
		key := transcriptKey(row)
		if headers[key] {
			selected = append(selected, row)
			continue
		}
		id := row.MessageUUID
		if !active[id] {
			continue
		}
		selected = append(selected, row)
		marker := piBranchMarker{Label: labels[id]}
		if n := len(children[id]); n > 1 {
			child := activeChild[id]
			choice := child
			if label := labels[child]; label != "" {
				choice = label
			}
			marker.Point = fmt.Sprintf("branch point: active path %q; %d alternate branch(es) hidden", choice, n-1)
		}
		if marker.Point != "" || marker.Label != "" {
			markers[key] = marker
		}
	}
	return selected, markers, ""
}

// --- Claude Code line shapes ---

type claudeEntry struct {
	Type    string `json:"type"`
	Message *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudeContentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"` // tool_result payload
	IsError  bool            `json:"is_error"`
}

func claudeBlocks(line []byte) []displayBlock {
	var e claudeEntry
	if err := json.Unmarshal(line, &e); err != nil {
		return nil
	}
	if e.Message == nil {
		if e.Type == "" {
			return nil
		}
		// Bookkeeping entries (mode, attachment, ai-title, …): a muted
		// one-liner keeps the transcript honest without drowning it.
		return []displayBlock{{Kind: "meta", Title: e.Type}}
	}

	// content may be a plain string or a block list.
	var text string
	if err := json.Unmarshal(e.Message.Content, &text); err == nil {
		return []displayBlock{textBlock("text", "", text)}
	}
	var parts []claudeContentBlock
	if err := json.Unmarshal(e.Message.Content, &parts); err != nil {
		return nil
	}
	var out []displayBlock
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, textBlock("text", "", p.Text))
		case "thinking":
			b := textBlock("thinking", "thinking", p.Thinking)
			b.Collapsed = true
			out = append(out, b)
		case "tool_use":
			b := textBlock("tool_use", "tool: "+p.Name, prettyJSON(p.Input))
			b.Collapsed = true
			out = append(out, b)
		case "tool_result":
			title := "tool result"
			if p.IsError {
				title = "tool result (error)"
			}
			b := textBlock("tool_result", title, toolResultText(p.Content))
			b.Collapsed = true
			out = append(out, b)
		default:
			b := textBlock("raw", "content block: "+p.Type, prettyJSON(mustCompact(p)))
			b.Collapsed = true
			out = append(out, b)
		}
	}
	return out
}

// toolResultText renders a tool_result content payload, which is a string or
// a list of typed blocks.
func toolResultText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []claudeContentBlock
	if err := json.Unmarshal(raw, &parts); err != nil {
		return prettyJSON(raw)
	}
	var buf bytes.Buffer
	for _, p := range parts {
		if p.Text != "" {
			if buf.Len() > 0 {
				buf.WriteString("\n")
			}
			buf.WriteString(p.Text)
		}
	}
	if buf.Len() == 0 {
		return prettyJSON(raw)
	}
	return buf.String()
}

// --- Codex rollout line shapes ---

type codexEntry struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

func codexBlocks(line []byte) []displayBlock {
	var e codexEntry
	if err := json.Unmarshal(line, &e); err != nil || e.Type == "" {
		return nil
	}
	title := e.Type
	if sub, ok := e.Payload["type"].(string); ok && sub != "" {
		title = e.Type + ": " + sub
	}
	if text := codexText(e.Payload); text != "" {
		b := textBlock("text", title, text)
		// Context/meta rollout entries stay collapsed; spoken turns show.
		if e.Type != "event_msg" && e.Type != "response_item" {
			b.Collapsed = true
		}
		return []displayBlock{b}
	}
	if len(e.Payload) == 0 {
		return []displayBlock{{Kind: "meta", Title: title}}
	}
	b := textBlock("raw", title, prettyJSON(mustCompact(e.Payload)))
	b.Collapsed = true
	return []displayBlock{b}
}

// codexText sniffs the common rollout text carriers without pretending to a
// full schema: payload.text, payload.message (string), or content items.
func codexText(payload map[string]any) string {
	if t, ok := payload["text"].(string); ok && t != "" {
		return t
	}
	if m, ok := payload["message"].(string); ok && m != "" {
		return m
	}
	items, ok := payload["content"].([]any)
	if !ok {
		return ""
	}
	var buf bytes.Buffer
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if t, ok := m["text"].(string); ok && t != "" {
			if buf.Len() > 0 {
				buf.WriteString("\n")
			}
			buf.WriteString(t)
		}
	}
	return buf.String()
}

// --- Grok chat_history line shapes ---

type grokEntry struct {
	Type      string          `json:"type"`
	Content   json.RawMessage `json:"content"`
	Summary   json.RawMessage `json:"summary"`
	ToolCalls []struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"tool_calls"`
}

func grokBlocks(line []byte) []displayBlock {
	var e grokEntry
	if err := json.Unmarshal(line, &e); err != nil || e.Type == "" {
		return nil
	}
	var out []displayBlock
	switch e.Type {
	case "system":
		if text := grokText(e.Content); text != "" {
			b := textBlock("text", "system prompt", text)
			b.Collapsed = true
			out = append(out, b)
		}
	case "user", "assistant":
		if text := grokText(e.Content); text != "" {
			out = append(out, textBlock("text", "", text))
		}
	case "reasoning":
		if text := grokText(e.Summary); text != "" {
			b := textBlock("thinking", "thinking", text)
			b.Collapsed = true
			out = append(out, b)
		}
	case "tool_result":
		if text := grokText(e.Content); text != "" {
			b := textBlock("tool_result", "tool result", text)
			b.Collapsed = true
			out = append(out, b)
		}
	default:
		return nil // unrecognized type: raw excerpt fallback
	}
	for _, tc := range e.ToolCalls {
		// arguments arrive as a JSON-encoded string; unwrap before pretty-
		// printing so the block shows the call, not an escaped blob.
		var args string
		pretty := ""
		if json.Unmarshal(tc.Arguments, &args) == nil {
			pretty = prettyJSON(json.RawMessage(args))
		} else {
			pretty = prettyJSON(tc.Arguments)
		}
		b := textBlock("tool_use", "tool: "+tc.Name, pretty)
		b.Collapsed = true
		out = append(out, b)
	}
	if len(out) == 0 {
		// A recognized type with an empty body (assistant lines that only
		// carry tool calls parse above; this is truly empty) stays honest.
		out = append(out, displayBlock{Kind: "meta", Title: e.Type})
	}
	return out
}

// grokText renders a grok content carrier: a plain string, or a list of typed
// parts carrying "text" (user content and reasoning summaries).
func grokText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return prettyJSON(raw)
	}
	var buf bytes.Buffer
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(p.Text)
	}
	return buf.String()
}

// --- Pi session-tree line shapes ---

type piEntry struct {
	Type          string          `json:"type"`
	Version       int             `json:"version"`
	CWD           string          `json:"cwd"`
	ParentSession string          `json:"parentSession"`
	Provider      string          `json:"provider"`
	ModelID       string          `json:"modelId"`
	ThinkingLevel string          `json:"thinkingLevel"`
	TargetID      string          `json:"targetId"`
	Label         string          `json:"label"`
	Summary       string          `json:"summary"`
	CustomType    string          `json:"customType"`
	Content       json.RawMessage `json:"content"`
	Message       *struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolName   string          `json:"toolName"`
		ToolCallID string          `json:"toolCallId"`
		IsError    bool            `json:"isError"`
	} `json:"message"`
}

type piContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func piBlocks(line []byte) []displayBlock {
	var e piEntry
	if json.Unmarshal(line, &e) != nil || e.Type == "" {
		return nil
	}
	switch e.Type {
	case "session":
		title := fmt.Sprintf("session format v%d · cwd %s", e.Version, e.CWD)
		if e.ParentSession != "" {
			title += " · forked from " + e.ParentSession
		}
		return []displayBlock{{Kind: "meta", Title: title}}
	case "message":
		if e.Message == nil {
			return nil
		}
		var text string
		if json.Unmarshal(e.Message.Content, &text) == nil {
			return []displayBlock{textBlock("text", "", text)}
		}
		var parts []piContentBlock
		if json.Unmarshal(e.Message.Content, &parts) != nil {
			return nil
		}
		var out []displayBlock
		for _, part := range parts {
			switch part.Type {
			case "text":
				out = append(out, textBlock("text", "", part.Text))
			case "thinking":
				b := textBlock("thinking", "thinking", part.Thinking)
				b.Collapsed = true
				out = append(out, b)
			case "toolCall":
				b := textBlock("tool_use", "tool: "+part.Name, prettyJSON(part.Arguments))
				b.Collapsed = true
				out = append(out, b)
			default:
				b := textBlock("raw", "content block: "+part.Type, prettyJSON(mustCompact(part)))
				b.Collapsed = true
				out = append(out, b)
			}
		}
		if e.Message.Role == "toolResult" {
			text := grokText(e.Message.Content)
			b := textBlock("tool_result", "tool result: "+e.Message.ToolName, text)
			b.Collapsed = true
			return []displayBlock{b}
		}
		return out
	case "model_change":
		return []displayBlock{{Kind: "meta", Title: "model: " + e.Provider + "/" + e.ModelID}}
	case "thinking_level_change":
		return []displayBlock{{Kind: "meta", Title: "thinking level: " + e.ThinkingLevel}}
	case "label":
		return []displayBlock{{Kind: "meta", Title: "label " + e.TargetID + ": " + e.Label}}
	case "branch_summary", "compaction":
		b := textBlock("meta", e.Type, e.Summary)
		b.Collapsed = true
		return []displayBlock{b}
	case "custom_message":
		return []displayBlock{textBlock("text", e.CustomType, grokText(e.Content))}
	default:
		return []displayBlock{{Kind: "meta", Title: e.Type}}
	}
}

// --- shared block helpers ---

func textBlock(kind, title, text string) displayBlock {
	cut, truncated := truncateText(text, maxBlockChars)
	return displayBlock{Kind: kind, Title: title, Text: cut, Truncated: truncated}
}

func truncateText(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	// Cut on a rune boundary so the template never escapes a torn rune.
	cut := limit
	for cut > 0 && text[cut]&0xC0 == 0x80 {
		cut--
	}
	return text[:cut], true
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

func mustCompact(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(fmt.Sprintf("%q", fmt.Sprint(v)))
	}
	return raw
}
