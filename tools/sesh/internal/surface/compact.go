package surface

import (
	"encoding/json"
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The compact view is the transcript page's default: prose for what people
// and agents said, a wrapped run of pills for the machinery between (tool
// calls with their results, thinking, hook context, files), and nothing at
// all for pure bookkeeping (turn timings, prompt snapshots, environment
// deltas). It is built from the same display entries the full view renders
// (?view=full), so the window, budget, and never-500 contracts are shared;
// only presentation differs. Every hidden line is still one click away in
// the full and raw views.

// Block presentations in the compact view.
const (
	presSay     = "say"     // prose message row
	presPill    = "pill"    // one pill in an activity run
	presHide    = "hide"    // bookkeeping: not drawn
	presDivider = "divider" // epoch boundary (context compaction)
)

// convRow is one compact-view row: a prose message, a run of pills, or a
// divider.
type convRow struct {
	Kind      string // "message", "run", "divider"
	Speaker   string
	Role      string // styling class: user, assistant, hcom, other
	Timestamp *time.Time
	Text      string
	Truncated bool
	Pills     []convPill
}

// convPill is one activity pill. A pill with a Detail or Result expands in
// place; consecutive identical detail-less pills fold into one with a count.
type convPill struct {
	Label           string
	Tone            string // tool, thinking, message, context, warn, error, other
	Count           int
	Detail          string
	DetailTruncated bool
	HasResult       bool
	Result          string
	ResultTruncated bool

	merge   string
	primary bool
	merged  bool
}

// Expandable reports whether the pill has anything to show when opened.
func (p convPill) Expandable() bool { return p.Detail != "" || p.HasResult }

// compactPres is a block's compact presentation: the parser's explicit
// choice when it made one, else derived from Kind. Untitled text is prose;
// every other kind is a pill so unknown shapes stay visible.
func compactPres(b displayBlock) string {
	if b.Pres != "" {
		return b.Pres
	}
	switch {
	case b.Kind == "text" && b.Title == "":
		return presSay
	case b.Kind == "meta" && b.Title == "" && b.Text == "" && !b.HasAlt:
		return presHide
	default:
		return presPill
	}
}

func kindTone(kind string) string {
	switch kind {
	case "tool_use", "tool_result":
		return "tool"
	case "thinking":
		return "thinking"
	default:
		return "other"
	}
}

func roleClass(speaker string) string {
	switch speaker {
	case "user", "assistant", "hcom":
		return speaker
	default:
		return "other"
	}
}

// compactRows folds a window's display entries into compact rows. Tool
// results attach to their call's pill when the call is in the same window;
// an orphaned result keeps its own pill.
func compactRows(entries []displayEntry) []convRow {
	var rows []convRow
	type pillRef struct{ row, pill int }
	calls := map[string]pillRef{}

	addPill := func(p convPill) pillRef {
		if len(rows) == 0 || rows[len(rows)-1].Kind != "run" {
			rows = append(rows, convRow{Kind: "run"})
		}
		run := &rows[len(rows)-1]
		if n := len(run.Pills); n > 0 && p.merge != "" {
			last := &run.Pills[n-1]
			if last.merge == p.merge && !last.merged && last.primary != p.primary {
				primary, other := *last, p
				if p.primary {
					primary, other = p, *last
				}
				if other.Tone == "message" {
					primary.Tone = "message"
				}
				primary.Detail, primary.DetailTruncated = other.Detail, other.DetailTruncated
				primary.merged, primary.Count = true, 1
				*last = primary
				return pillRef{len(rows) - 1, n - 1}
			}
		}
		if n := len(run.Pills); n > 0 && p.Tone == "context" && !p.HasResult {
			// Back-to-back injected context (a session's preamble) is one
			// pill: the details concatenate.
			last := &run.Pills[n-1]
			if last.Label == p.Label && last.Tone == p.Tone && !last.HasResult && last.merge == "" && p.merge == "" {
				if p.Detail != "" {
					last.Detail = strings.TrimLeft(last.Detail+"\n\n"+p.Detail, "\n")
				}
				last.DetailTruncated = last.DetailTruncated || p.DetailTruncated
				last.Count++
				return pillRef{len(rows) - 1, n - 1}
			}
		}
		if n := len(run.Pills); n > 0 && !p.Expandable() {
			last := &run.Pills[n-1]
			if !last.Expandable() && last.Label == p.Label && last.Tone == p.Tone {
				last.Count++
				return pillRef{len(rows) - 1, n - 1}
			}
		}
		p.Count = 1
		run.Pills = append(run.Pills, p)
		return pillRef{len(rows) - 1, len(run.Pills) - 1}
	}

	for _, e := range entries {
		if e.BranchPoint != "" {
			addPill(convPill{Label: e.BranchPoint, Tone: "warn"})
		}
		if e.BranchLabel != "" {
			addPill(convPill{Label: "branch label: " + e.BranchLabel, Tone: "other"})
		}
		switch {
		case e.Quarantined:
			p := convPill{Label: "quarantined line", Tone: "warn"}
			if e.QuarantineReason != "" {
				p.Label += ": " + e.QuarantineReason
			}
			if len(e.Blocks) > 0 {
				p.Detail, p.DetailTruncated = compactText(e.Blocks[0])
			}
			addPill(p)
			continue
		case e.Truncated:
			addPill(convPill{Label: "oversized line truncated (" + fmtSize(e.ByteSize) + ") — full bytes in raw view", Tone: "warn"})
			continue
		}
		for _, b := range e.Blocks {
			switch compactPres(b) {
			case presHide:
			case presDivider:
				label := b.Label
				if label == "" {
					label = b.Title
				}
				rows = append(rows, convRow{Kind: "divider", Text: label, Timestamp: e.Timestamp})
			case presSay:
				speaker := b.Speaker
				if speaker == "" {
					speaker = e.Role
				}
				text, truncated := compactText(b)
				if speaker == "assistant" {
					var notes []convPill
					notes, text = splitFencing(text)
					for _, p := range notes {
						addPill(p)
					}
				}
				text = strings.TrimSpace(text)
				if text == "" {
					continue
				}
				// A hook-delivered message supersedes its one-line notice.
				if n := len(rows); b.Merge != "" && n > 0 && rows[n-1].Kind == "run" {
					run := &rows[n-1]
					if k := len(run.Pills); k > 0 && run.Pills[k-1].merge == b.Merge && run.Pills[k-1].primary && !run.Pills[k-1].merged {
						run.Pills = run.Pills[:k-1]
						if len(run.Pills) == 0 {
							rows = rows[:n-1]
						}
					}
				}
				rows = append(rows, convRow{Kind: "message", Speaker: speaker, Role: roleClass(speaker),
					Timestamp: e.Timestamp, Text: text, Truncated: truncated})
			default:
				text, truncated := compactText(b)
				if b.Kind == "tool_result" && b.ToolID != "" {
					if ref, ok := calls[b.ToolID]; ok {
						p := &rows[ref.row].Pills[ref.pill]
						p.HasResult, p.Result, p.ResultTruncated = true, text, truncated
						if b.IsError {
							p.Tone = "error"
						}
						continue
					}
				}
				label := b.Label
				if label == "" {
					label = b.Title
				}
				if label == "" {
					label = b.Kind
				}
				tone := b.Tone
				if tone == "" {
					tone = kindTone(b.Kind)
				}
				if b.IsError {
					tone = "error"
				}
				ref := addPill(convPill{Label: label, Tone: tone, Detail: text, DetailTruncated: truncated,
					merge: b.Merge, primary: b.Primary})
				if b.Kind == "tool_use" && b.ToolID != "" {
					calls[b.ToolID] = ref
				}
			}
		}
	}
	return rows
}

// fencingSegment matches the fleet's chat fencing: <status>…</status> is a
// one-line progress note, <internal>…</internal> working notes.
var fencingSegment = regexp.MustCompile(`(?s)<(status|internal)>(.*?)</(status|internal)>`)

// splitFencing lifts fenced status and internal notes out of assistant prose
// into pills, returning the prose that remains. Text without fencing passes
// through untouched; mismatched tags are left as prose.
func splitFencing(text string) ([]convPill, string) {
	if !strings.Contains(text, "<status>") && !strings.Contains(text, "<internal>") {
		return nil, text
	}
	var pills []convPill
	rest := fencingSegment.ReplaceAllStringFunc(text, func(m string) string {
		sub := fencingSegment.FindStringSubmatch(m)
		if sub[1] != sub[3] {
			return m
		}
		body := strings.TrimSpace(sub[2])
		if sub[1] == "status" {
			p := convPill{Label: oneLine(body, 120), Tone: "status"}
			if p.Label != strings.Join(strings.Fields(body), " ") {
				p.Detail = body
			}
			pills = append(pills, p)
		} else {
			pills = append(pills, convPill{Label: "internal note", Tone: "thinking", Detail: body})
		}
		return ""
	})
	return pills, rest
}

// --- Claude classification ---

// claudeHiddenAttachments are attachment kinds that carry harness context
// (prompt snapshots, tool and skill listings, reminders, environment
// deltas), never conversation.
var claudeHiddenAttachments = map[string]bool{
	"agent_listing_delta":       true,
	"auto_mode":                 true,
	"bash_output_audience_note": true,
	"batching_reminder_sent":    true,
	"command_permissions":       true,
	"credential_org":            true,
	"date":                      true,
	"date_change":               true,
	"deferred_tools_delta":      true,
	"deferred_tools_record":     true,
	"environment":               true,
	"hook_success":              true,
	"instructions":              true,
	"invoked_skills":            true,
	"mcp_instructions_delta":    true,
	"model":                     true,
	"prompt_snapshot":           true,
	"read_truncation_notice":    true,
	"remote_session_change":     true,
	"session_context":           true,
	"silent_turn_reminder":      true,
	"skill_listing":             true,
	"task_reminder":             true,
	"total_tokens_reminder":     true,
}

type claudeAttachment struct {
	Type        string          `json:"type"`
	Content     json.RawMessage `json:"content"`
	HookName    string          `json:"hookName"`
	HookEvent   string          `json:"hookEvent"`
	DisplayPath string          `json:"displayPath"`
	Filename    string          `json:"filename"`
	Path        string          `json:"path"`
	Snippet     json.RawMessage `json:"snippet"`
	Prompt      json.RawMessage `json:"prompt"`
	Response    json.RawMessage `json:"response"`
	Stdout      string          `json:"stdout"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Delta       json.RawMessage `json:"deltaSummary"`
}

// setAlt sets the compact view's text for a block, leaving the full view's
// Text byte-faithful.
func setAlt(b *displayBlock, text string) {
	b.HasAlt = true
	b.Alt, b.AltTruncated = truncateText(text, maxBlockChars)
}

// compactText is the text the compact view draws for a block.
func compactText(b displayBlock) (string, bool) {
	if b.HasAlt {
		return b.Alt, b.AltTruncated
	}
	return b.Text, b.Truncated
}

// classifyClaudeBookkeeping sets the compact presentation of a message-less
// Claude line. Unknown types and attachment kinds stay visible as pills.
func classifyClaudeBookkeeping(b *displayBlock, e claudeEntry) {
	switch {
	case e.Type == "attachment":
		classifyClaudeAttachment(b, e.Attachment)
	case e.Type == "system":
		classifyClaudeSystem(b, e)
	case isClaudeSidecarEntryType(e.Type):
		b.Pres = presHide
	}
}

func classifyClaudeAttachment(b *displayBlock, raw json.RawMessage) {
	var a claudeAttachment
	if err := json.Unmarshal(raw, &a); err != nil || a.Type == "" {
		return
	}
	if claudeHiddenAttachments[a.Type] {
		b.Pres = presHide
		return
	}
	b.Pres, b.Tone = presPill, "context"
	switch a.Type {
	case "hook_additional_context", "hook_system_message":
		// One hook firing writes both: the system message is the one-line
		// notice the operator saw, the additional context what the model
		// read. They fold into one pill labelled by the notice.
		text := strings.TrimSpace(looseText(a.Content))
		if text == "" {
			b.Pres = presHide
			return
		}
		setAlt(b, text)
		b.Merge = "hook:" + a.HookEvent
		switch {
		case a.Type == "hook_system_message":
			b.Label, b.Primary = oneLine(text, 140), true
		case strings.HasPrefix(text, "<hcom>"):
			classifyHcomDelivery(b, text)
		default:
			b.Label = "hook context · " + firstNonEmpty(a.HookEvent, a.HookName)
		}
	case "async_hook_response":
		setAlt(b, strings.TrimSpace(looseText(a.Response)+"\n"+a.Stdout))
		b.Label = "hook response · " + firstNonEmpty(a.HookName, a.HookEvent)
	case "file", "nested_memory":
		b.Label = "file · " + firstNonEmpty(a.DisplayPath, a.Filename, a.Path)
		setAlt(b, looseText(a.Content))
	case "compact_file_reference":
		b.Label = "file · " + firstNonEmpty(a.DisplayPath, a.Filename)
	case "edited_text_file":
		b.Label = "edited · " + firstNonEmpty(a.DisplayPath, a.Filename)
		setAlt(b, looseText(a.Snippet))
	case "queued_command":
		b.Pres, b.Speaker = presSay, "user"
		setAlt(b, looseText(a.Prompt))
	case "task_status":
		b.Label = "task · " + oneLine(firstNonEmpty(a.Description, "background task"), 80)
		if a.Status != "" {
			b.Label += " · " + a.Status
		}
		setAlt(b, looseText(a.Delta))
	default:
		b.Label, b.Tone = "attachment · "+a.Type, "other"
		setAlt(b, prettyJSON(raw))
	}
}

func classifyClaudeSystem(b *displayBlock, e claudeEntry) {
	text := strings.TrimSpace(looseText(e.Content))
	b.Pres, b.Tone = presPill, "other"
	switch e.Subtype {
	case "turn_duration", "stop_hook_summary":
		b.Pres = presHide
		return
	case "compact_boundary":
		b.Pres, b.Label = presDivider, "context compacted"
		return
	case "away_summary":
		b.Label = "recap"
	case "scheduled_task_fire":
		b.Label = "scheduled task"
	case "local_command":
		b.Label = "command output"
	case "informational":
		b.Label = "info"
	case "api_error", "model_refusal_fallback", "model_refusal_no_fallback":
		b.Label, b.Tone = e.Subtype, "error"
	default:
		b.Label = "system"
		if e.Subtype != "" {
			b.Label += " · " + e.Subtype
		}
	}
	setAlt(b, text)
}

var commandTag = regexp.MustCompile(`(?s)<command-(name|args)>(.*?)</command-(?:name|args)>`)

// classifyClaudeText sorts a Claude message text block: assistant text and
// genuine user prompts are prose; harness-injected user text (slash-command
// echoes, command output, skill bodies, reminders, compaction summaries)
// becomes a pill. A user line wrapped in <hcom> is a bus delivery and reads
// as prose from "hcom".
func classifyClaudeText(b *displayBlock, e claudeEntry, text string) {
	if e.Message == nil || e.Message.Role != "user" {
		return
	}
	t := strings.TrimSpace(text)
	pill := func(label, tone string, detail string) {
		b.Pres, b.Label, b.Tone = presPill, label, tone
		setAlt(b, detail)
	}
	switch {
	case e.IsCompactSummary:
		pill("compaction summary", "context", text)
	case strings.HasPrefix(t, "<command-name>") || strings.HasPrefix(t, "<command-message>"):
		var name, args string
		for _, m := range commandTag.FindAllStringSubmatch(t, -1) {
			if m[1] == "name" {
				name = strings.TrimSpace(m[2])
			} else {
				args = strings.TrimSpace(m[2])
			}
		}
		pill(oneLine(strings.TrimSpace(firstNonEmpty(name, "command")+" "+args), 100), "tool", "")
	case strings.HasPrefix(t, "<local-command-caveat>"):
		b.Pres = presHide
	case strings.HasPrefix(t, "<local-command-stdout>") || strings.HasPrefix(t, "<local-command-stderr>"):
		out := stripTags(t, "local-command-stdout", "local-command-stderr")
		if out == "" {
			b.Pres = presHide
			return
		}
		pill("command output", "tool", out)
	case strings.HasPrefix(t, "<task-notification>"):
		pill("task notification", "context", stripTags(t, "task-notification"))
	case strings.HasPrefix(t, "[Request interrupted"):
		pill("interrupted", "warn", "")
	case strings.HasPrefix(t, "<hcom>"):
		b.Pres, b.Speaker = presSay, "hcom"
		setAlt(b, stripTags(t, "hcom"))
	case strings.HasPrefix(t, "<system-reminder>"):
		pill("system reminder", "context", stripTags(t, "system-reminder"))
	case e.IsMeta:
		pill("injected context", "context", text)
	}
}

// toolUseLabel is a tool call's pill text: the tool name and its most
// telling argument (a Bash description beats its command; a path beats the
// rest), first line only.
func toolUseLabel(name string, input json.RawMessage) string {
	if i := strings.LastIndex(name, "__"); strings.HasPrefix(name, "mcp__") && i >= 0 {
		name = name[i+2:]
	}
	var args map[string]any
	if json.Unmarshal(input, &args) != nil {
		return name
	}
	for _, k := range []string{"description", "command", "file_path", "notebook_path", "path", "pattern", "url", "query", "skill", "subagent_type", "prompt", "message", "text"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			arg := oneLine(v, 90)
			if strings.HasSuffix(k, "path") && len(v) > 90 {
				arg = "…" + v[len(v)-89:]
			}
			return name + ": " + arg
		}
	}
	return name
}

// looseText extracts readable text from a JSON value of unknown shape: a
// string, a list of text blocks, or an object carrying text/content (a file
// attachment nests it under file.content). Anything else is pretty JSON.
func looseText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		var parts []string
		for _, item := range list {
			if t := looseText(item); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		for _, k := range []string{"text", "content", "file"} {
			if v, ok := obj[k]; ok {
				if t := looseText(v); t != "" {
					return t
				}
			}
		}
	}
	return prettyJSON(raw)
}

// operatorNote is the boilerplate herder web wraps around an operator's
// message; the message itself follows it.
var operatorNote = regexp.MustCompile(`(?s)\[HERDER_WEB_OPERATOR_NOTE_BEGIN\].*?\[HERDER_WEB_OPERATOR_NOTE_END\]\s*`)

func stripOperatorNote(text string) string {
	return strings.TrimSpace(operatorNote.ReplaceAllString(text, ""))
}

func stripTags(text string, tags ...string) string {
	for _, tag := range tags {
		text = strings.ReplaceAll(text, "<"+tag+">", "")
		text = strings.ReplaceAll(text, "</"+tag+">", "")
	}
	return strings.TrimSpace(text)
}

// oneLine is the first non-blank line of text with runs of whitespace
// collapsed, cut to limit runes with an ellipsis.
func oneLine(text string, limit int) string {
	line := ""
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			line = l
			break
		}
	}
	line = strings.Join(strings.Fields(line), " ")
	if r := []rune(line); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return line
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// fmtClock is a message row's time: HH:MM:SS UTC, empty when unknown.
func fmtClock(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format("15:04:05")
}

// viewURL adds the full-view selector to a transcript URL.
func viewURL(u string, full bool) string {
	if !full || u == "" {
		return u
	}
	if strings.Contains(u, "?") {
		return u + "&view=full"
	}
	return u + "?view=full"
}

var (
	proseBold    = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	proseHeading = regexp.MustCompile(`(?m)^#{1,6} +(.+)$`)
	proseSpan    = regexp.MustCompile("\x00[0-9]+\x00")
)

// prose renders message text with the markdown marks that most hurt when
// left raw: ``` fenced blocks, `code` spans, **bold** and # headings (as
// bold). Everything is HTML-escaped first; only those fixed tags are
// inserted, and line structure is left to white-space: pre-wrap.
func prose(text string) template.HTML {
	fences := strings.Split(text, "```")
	if len(fences)%2 == 0 {
		// An unclosed fence: treat the text as having none.
		fences = []string{text}
	}
	var b strings.Builder
	for i, part := range fences {
		if i%2 == 1 {
			// Drop the info string (```go) and the newlines hugging the fence.
			if nl := strings.IndexByte(part, '\n'); nl >= 0 && !strings.ContainsAny(part[:nl], " \t") {
				part = part[nl+1:]
			}
			b.WriteString(`<code class="fence">` + html.EscapeString(strings.TrimRight(part, "\n")) + "</code>")
			continue
		}
		if i > 0 {
			part = strings.TrimPrefix(part, "\n")
		}
		proseInline(&b, part)
	}
	return template.HTML(b.String())
}

func proseInline(b *strings.Builder, text string) {
	parts := strings.Split(text, "`")
	if len(parts)%2 == 0 {
		// An unpaired backtick: no code spans at all rather than a guess.
		parts = []string{text}
	}
	// Code spans become NUL-delimited placeholders so bold can wrap them
	// (**see `x`**) without bold marks inside code ever matching.
	var spans []string
	var flat strings.Builder
	for i, part := range parts {
		if i%2 == 1 {
			flat.WriteString("\x00" + strconv.Itoa(len(spans)) + "\x00")
			spans = append(spans, "<code>"+html.EscapeString(part)+"</code>")
			continue
		}
		flat.WriteString(html.EscapeString(strings.ReplaceAll(part, "\x00", "")))
	}
	esc := proseHeading.ReplaceAllString(flat.String(), "<strong>$1</strong>")
	esc = proseBold.ReplaceAllString(esc, "<strong>$1</strong>")
	b.WriteString(proseSpan.ReplaceAllStringFunc(esc, func(m string) string {
		n, _ := strconv.Atoi(m[1 : len(m)-1])
		return spans[n]
	}))
}

// --- Codex rollouts ---

// codexHidden are rollout lines that are pure bookkeeping in a reading
// view: session/turn settings, token accounting, lifecycle events, and the
// event_msg echoes of messages the response_item stream already carries.
var codexHidden = map[string]bool{
	"session_meta": true, "turn_context": true, "world_state": true, "token_usage_record": true,
	"event_msg: token_count": true, "event_msg: task_started": true, "event_msg: task_complete": true,
	"event_msg: item_completed": true, "event_msg: item_started": true, "event_msg: thread_settings_applied": true,
	"event_msg: user_message": true, "event_msg: agent_message": true, "event_msg: agent_reasoning": true,
	"event_msg: exec_command_end": true, "event_msg: patch_apply_end": true,
}

// codexInjected marks user-role messages that are harness context, not
// something a person typed.
var codexInjected = []string{"# AGENTS.md", "<environment_context>", "<user_instructions>", "<INSTRUCTIONS>", "<permissions"}

// codexExecCmd pulls the first shell command out of an exec tool script.
var codexExecCmd = regexp.MustCompile(`cmd:\s*"((?:[^"\\]|\\.)*)"`)

func classifyCodex(b *displayBlock, e codexEntry) {
	sub, _ := e.Payload["type"].(string)
	key := e.Type
	if e.Type == "event_msg" || sub != "" && e.Type != "response_item" {
		key = e.Type + ": " + sub
	}
	if codexHidden[key] {
		b.Pres = presHide
		return
	}
	if e.Type == "compacted" {
		b.Pres, b.Label = presDivider, "context compacted"
		return
	}
	if e.Type != "response_item" {
		return
	}
	str := func(k string) string { s, _ := e.Payload[k].(string); return s }
	switch sub {
	case "message":
		text := b.Text
		switch str("role") {
		case "assistant":
			b.Pres, b.Speaker = presSay, "assistant"
		case "user":
			switch {
			case strings.HasPrefix(text, "<hcom>"):
				classifyHcomDelivery(b, text)
			case hasAnyPrefix(strings.TrimSpace(text), codexInjected):
				b.Label, b.Tone = "injected context", "context"
			default:
				b.Pres, b.Speaker = presSay, "user"
			}
		case "developer":
			if strings.HasPrefix(text, "<hcom>") {
				classifyHcomDelivery(b, text)
			} else {
				b.Label, b.Tone = "developer instructions", "context"
			}
		}
	case "reasoning":
		var parts []string
		if items, ok := e.Payload["summary"].([]any); ok {
			for _, it := range items {
				if m, ok := it.(map[string]any); ok {
					if t, _ := m["text"].(string); t != "" {
						parts = append(parts, t)
					}
				}
			}
		}
		if len(parts) == 0 {
			b.Pres = presHide // encrypted only: nothing to read
			return
		}
		b.Kind, b.Label = "thinking", "thinking"
		setAlt(b, strings.Join(parts, "\n"))
	case "custom_tool_call", "function_call":
		name := str("name")
		input := firstNonEmpty(str("input"), str("arguments"))
		b.Kind, b.ToolID = "tool_use", str("call_id")
		if m := codexExecCmd.FindStringSubmatch(input); name == "exec" && m != nil {
			var cmd string
			if json.Unmarshal([]byte(`"`+m[1]+`"`), &cmd) != nil {
				cmd = m[1]
			}
			b.Label = "exec: " + oneLine(cmd, 90)
		} else if json.Valid([]byte(input)) {
			b.Label = toolUseLabel(name, json.RawMessage(input))
		} else {
			b.Label = name
		}
		if json.Valid([]byte(input)) {
			setAlt(b, prettyJSON(json.RawMessage(input)))
		} else {
			setAlt(b, input)
		}
	case "custom_tool_call_output", "function_call_output":
		b.Kind, b.ToolID = "tool_result", str("call_id")
		out, _ := json.Marshal(e.Payload["output"])
		setAlt(b, looseText(out))
	}
}

// classifyHcomDelivery presents an <hcom> bus delivery. A request is
// someone asking this agent for something, so it reads as a message;
// informs and acks stay pills.
var hcomRequest = regexp.MustCompile(`^\[request[ :]`)

func classifyHcomDelivery(b *displayBlock, text string) {
	msg := stripOperatorNote(stripTags(text, "hcom"))
	if msg == "" {
		b.Pres = presHide
		return
	}
	b.Pres, b.Label, b.Tone = presPill, "✉ "+oneLine(msg, 140), "message"
	setAlt(b, msg)
	if hcomRequest.MatchString(msg) {
		b.Pres, b.Speaker = presSay, "hcom"
	}
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// --- Grok chat history ---

// classifyGrokUser keeps what a person typed (<user_query>) as prose and
// pills the context Grok injects as user turns.
func classifyGrokUser(b *displayBlock, e grokEntry, text string) {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(t, "<user_query>"):
		setAlt(b, stripTags(t, "user_query"))
	case strings.HasPrefix(t, "<hcom>"):
		classifyHcomDelivery(b, t)
	case e.SyntheticReason != "" || strings.HasPrefix(t, "<"):
		b.Pres, b.Label, b.Tone = presPill, firstNonEmpty(strings.ReplaceAll(e.SyntheticReason, "_", " "), "injected context"), "context"
	}
}
