package claudesession

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxToolOutputBytes = 16 * 1024

var bookkeepingTypes = map[string]struct{}{
	"agent-name": {}, "ai-title": {}, "bridge-session": {},
	"file-history-delta": {}, "file-history-snapshot": {},
	"last-prompt": {}, "mode": {},
	"permission-mode": {}, "pr-link": {}, "queue-operation": {},
	"worktree-state": {},
}

var hcomMessagePattern = regexp.MustCompile(`(?m)\[([^\]#]+?)\s+#(\d+)\]\s+(\S+)\s+→\s+(.+?):[ \t]+`)

type envelope struct {
	Type             string `json:"type"`
	Subtype          string `json:"subtype"`
	UUID             string `json:"uuid"`
	Timestamp        string `json:"timestamp"`
	IsSidechain      bool   `json:"isSidechain"`
	IsMeta           bool   `json:"isMeta"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	PromptSource     string `json:"promptSource"`
	Origin           struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Attachment struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
	} `json:"attachment"`
}

// ReadFrom emits every renderable complete line at or after offset. A
// trailing line without '\n' is held back and NextOffset remains before it.
func ReadFrom(path string, offset int64) (ReadResult, error) {
	if offset < 0 {
		return ReadResult{}, fmt.Errorf("negative session offset: %d", offset)
	}
	f, err := os.Open(path)
	if err != nil {
		return ReadResult{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ReadResult{}, err
	}
	if offset > st.Size() {
		return ReadResult{}, fmt.Errorf("session offset %d beyond size %d", offset, st.Size())
	}
	line, err := lineAt(f, offset)
	if err != nil {
		return ReadResult{}, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ReadResult{}, err
	}

	result := ReadResult{NextOffset: offset}
	reader := bufio.NewReader(f)
	for {
		start := result.NextOffset
		raw, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ReadResult{}, err
		}
		result.NextOffset += int64(len(raw))
		body := bytes.TrimSuffix(raw[:len(raw)-1], []byte{'\r'})
		entry, render, sidechain := classify(body, line, start)
		if sidechain {
			result.Stats.SidechainSkipped++
		} else if render {
			result.Entries = append(result.Entries, entry)
		}
		line++
	}
	return result, nil
}

func lineAt(f *os.File, offset int64) (int64, error) {
	if offset == 0 {
		return 0, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	buf := make([]byte, 32*1024)
	var line, read int64
	for read < offset {
		want := min(int64(len(buf)), offset-read)
		n, err := io.ReadFull(f, buf[:want])
		line += int64(bytes.Count(buf[:n], []byte{'\n'}))
		read += int64(n)
		if err != nil {
			return 0, err
		}
	}
	return line, nil
}

func classify(raw []byte, line, offset int64) (Entry, bool, bool) {
	base := Entry{Line: line, ByteOffset: offset, Kind: KindUnknown}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		base.Payload = mustJSON(map[string]any{"raw": string(raw)})
		base.Quarantine = &Quarantine{Reason: "invalid_json"}
		return base, true, false
	}
	base.UUID, base.Timestamp = env.UUID, env.Timestamp
	base.Payload = append(json.RawMessage(nil), raw...)
	if env.IsSidechain {
		return Entry{}, false, true
	}
	if isBookkeeping(env.Type) {
		return Entry{}, false, false
	}

	switch env.Type {
	case "assistant":
		base.Kind, base.Payload = classifyAssistant(env.Message.Content, raw)
	case "user", "message":
		base.Kind, base.Payload = classifyUser(env, raw)
	case "attachment":
		base.Kind, base.Payload = classifyAttachment(env, raw)
	case "system":
		switch env.Subtype {
		case "turn_duration":
			base.Kind = KindTurnDuration
		case "compact_boundary":
			base.Kind = KindCompactDivider
		case "scheduled_task_fire", "stop_hook_summary", "away_summary", "informational":
			base.Kind = KindSystemChip
		default:
			base.Kind = KindUnknown
		}
	default:
		base.Kind = KindUnknown
	}
	return base, true, false
}

func isBookkeeping(typ string) bool {
	_, ok := bookkeepingTypes[typ]
	return ok
}

func classifyAssistant(content, raw json.RawMessage) (Kind, json.RawMessage) {
	blocks := contentBlocks(content)
	if len(blocks) == 0 {
		return KindUnknown, raw
	}
	switch stringValue(blocks[0], "type") {
	case "text":
		return KindAssistantText, raw
	case "thinking":
		return KindThinking, raw
	case "tool_use":
		payload := map[string]any{
			"tool_use_id": stringValue(blocks[0], "id"),
			"name":        stringValue(blocks[0], "name"),
			"input":       rawValue(blocks[0], "input"),
		}
		return KindToolUse, mustJSON(payload)
	default:
		return KindUnknown, raw
	}
}

func classifyUser(env envelope, raw json.RawMessage) (Kind, json.RawMessage) {
	blocks := contentBlocks(env.Message.Content)
	if len(blocks) > 0 && stringValue(blocks[0], "type") == "tool_result" {
		return KindToolResult, normalizeToolResult(blocks[0], rawValueFromRaw(raw, "toolUseResult"))
	}
	text := contentText(env.Message.Content)
	if env.IsCompactSummary || strings.HasPrefix(text, "This session is being continued") {
		return KindCompactDivider, raw
	}
	if env.IsMeta {
		return KindInjectedSystem, raw
	}
	if env.Origin.Kind == "task-notification" {
		return KindTaskNotification, raw
	}
	if strings.TrimSpace(text) == "<hcom>" {
		return KindHcomStub, raw
	}
	if strings.Contains(text, "<command-name>") || strings.Contains(text, "<local-command-stdout>") {
		return KindCommandOutput, raw
	}
	if env.Origin.Kind == "human" && (env.PromptSource == "typed" || env.PromptSource == "queued") {
		return KindHumanPrompt, raw
	}
	return KindUnknown, raw
}

func classifyAttachment(env envelope, raw json.RawMessage) (Kind, json.RawMessage) {
	if env.Attachment.Type != "hook_system_message" && env.Attachment.Type != "hook_additional_context" {
		return KindSystemChip, raw
	}
	body := unwrapHcom(attachmentText(env.Attachment.Content))
	matches := hcomMessagePattern.FindAllStringSubmatchIndex(body, -1)
	deliveries := make([]map[string]any, 0, max(len(matches), 1))
	for i, match := range matches {
		intent, thread, _ := strings.Cut(strings.TrimSpace(body[match[2]:match[3]]), ":")
		textEnd := len(body)
		if i+1 < len(matches) {
			textEnd = matches[i+1][0]
		}
		delivery := map[string]any{
			"intent":     strings.TrimSpace(intent),
			"message_id": body[match[4]:match[5]],
			"sender":     body[match[6]:match[7]],
			"recipient":  strings.TrimSpace(body[match[8]:match[9]]),
			"text":       strings.TrimSpace(body[match[1]:textEnd]),
		}
		if thread != "" {
			delivery["thread"] = strings.TrimSpace(thread)
		}
		deliveries = append(deliveries, delivery)
	}
	if len(deliveries) == 0 {
		deliveries = append(deliveries, map[string]any{"text": strings.TrimSpace(body)})
	}
	return KindHcomDelivery, mustJSON(map[string]any{
		"subtype": env.Attachment.Type, "deliveries": deliveries,
	})
}

func attachmentText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []string
	if json.Unmarshal(raw, &parts) == nil {
		return strings.Join(parts, "\n")
	}
	return ""
}

func unwrapHcom(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "<hcom>")
	text = strings.TrimSuffix(text, "</hcom>")
	return strings.TrimSpace(text)
}

func normalizeToolResult(block map[string]json.RawMessage, toolUseResult json.RawMessage) json.RawMessage {
	content := rawValue(block, "content")
	remaining := maxToolOutputBytes
	total := 0
	truncated := false
	images := 0
	var normalized any

	var text string
	if json.Unmarshal(content, &text) == nil {
		total = len([]byte(text))
		text, truncated = capText(text, remaining)
		normalized = text
	} else {
		var rawBlocks []map[string]json.RawMessage
		if json.Unmarshal(content, &rawBlocks) == nil {
			out := make([]any, 0, len(rawBlocks))
			for _, b := range rawBlocks {
				typ := stringValue(b, "type")
				switch typ {
				case "image":
					images++
					out = append(out, map[string]any{"type": "image", "present": true})
				case "text":
					value := stringValue(b, "text")
					total += len([]byte(value))
					capped, cut := capText(value, remaining)
					remaining -= len([]byte(capped))
					truncated = truncated || cut
					out = append(out, map[string]any{"type": "text", "text": capped})
				default:
					encoded, _ := json.Marshal(b)
					total += len(encoded)
					value, cut := capText(string(encoded), remaining)
					remaining -= len([]byte(value))
					truncated = truncated || cut
					out = append(out, map[string]any{"type": typ, "raw": value})
				}
			}
			normalized = out
		} else {
			total = len(content)
			value, cut := capText(string(content), remaining)
			truncated = cut
			normalized = value
		}
	}

	payload := map[string]any{
		"tool_use_id": stringValue(block, "tool_use_id"),
		"is_error":    boolValue(block, "is_error"),
		"content":     normalized,
		"total_bytes": total,
		"truncated":   truncated,
		"image_count": images,
	}
	if len(toolUseResult) > 0 && string(toolUseResult) != "null" {
		payload["toolUseResult"] = toolUseResult
	}
	return mustJSON(payload)
}

func capText(value string, limit int) (string, bool) {
	b := []byte(value)
	if len(b) <= limit {
		return value, false
	}
	end := min(max(limit, 0), len(b))
	// Only repair a split UTF-8 sequence at the cap boundary. Invalid bytes
	// earlier in binary-ish output must not make the result collapse toward
	// that byte; at most one UTF-8 sequence (three bytes) is held back.
	floor := max(end-3, 0)
	for end > floor && end < len(b) && !utf8.RuneStart(b[end]) {
		end--
	}
	return string(b[:end]), true
}

func contentBlocks(raw json.RawMessage) []map[string]json.RawMessage {
	var blocks []map[string]json.RawMessage
	_ = json.Unmarshal(raw, &blocks)
	return blocks
}

func contentText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	for _, block := range contentBlocks(raw) {
		if stringValue(block, "type") == "text" {
			return stringValue(block, "text")
		}
	}
	return ""
}

func rawValue(m map[string]json.RawMessage, key string) json.RawMessage {
	if value, ok := m[key]; ok {
		return append(json.RawMessage(nil), value...)
	}
	return json.RawMessage("null")
}

func rawValueFromRaw(raw json.RawMessage, key string) json.RawMessage {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil
	}
	return rawValue(object, key)
}

func stringValue(m map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(m[key], &value)
	return value
}

func boolValue(m map[string]json.RawMessage, key string) bool {
	var value bool
	_ = json.Unmarshal(m[key], &value)
	return value
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
