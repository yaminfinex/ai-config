package codexsession

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxToolOutputBytes = 16 * 1024

var (
	hcomMessagePattern = regexp.MustCompile(`(?m)\[([^\]#]+?)\s+#(\d+)\]\s+(\S+)\s+→\s+(.+?):[ \t]+`)
	exitCodePattern    = regexp.MustCompile(`(?m)^Process exited with code ([0-9]+)$`)
	toolExitPattern    = regexp.MustCompile(`(?m)^Exit code: ([0-9]+)$`)
)

var knownEventTypes = map[string]struct{}{
	"agent_message": {}, "agent_reasoning": {}, "collab_agent_interaction_end": {},
	"collab_agent_spawn_end": {}, "collab_close_end": {}, "collab_resume_end": {},
	"collab_waiting_end": {}, "context_compacted": {}, "entered_review_mode": {},
	"exec_command_end": {}, "exited_review_mode": {}, "mcp_tool_call_end": {},
	"patch_apply_end": {}, "sub_agent_activity": {}, "task_started": {},
	"thread_goal_updated": {}, "thread_rolled_back": {}, "thread_settings_applied": {},
	"token_count": {}, "turn_aborted": {}, "web_search_end": {},
}

type rolloutEnvelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type responsePayload struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Phase     string          `json:"phase"`
	Name      string          `json:"name"`
	CallID    string          `json:"call_id"`
	Arguments json.RawMessage `json:"arguments"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Content   json.RawMessage `json:"content"`
	Summary   json.RawMessage `json:"summary"`
	Action    json.RawMessage `json:"action"`
	Tools     json.RawMessage `json:"tools"`
	Status    string          `json:"status"`
}

type eventPayload struct {
	Type             string          `json:"type"`
	Message          string          `json:"message"`
	DurationMS       int64           `json:"duration_ms"`
	LastAgentMessage string          `json:"last_agent_message"`
	Raw              json.RawMessage `json:"-"`
}

type offsetBeyondError struct{ offset, size int64 }

func (e *offsetBeyondError) Error() string {
	return fmt.Sprintf("session offset %d beyond size %d", e.offset, e.size)
}

// ReadFrom emits every renderable complete line at or after offset. A partial
// trailing line is held back and NextOffset remains before it.
func ReadFrom(path string, offset int64) (ReadResult, error) {
	return read(path, offset, 0, false)
}

// ReadWindow emits at most limit renderable entries at or after offset.
func ReadWindow(path string, offset int64, limit int) (ReadResult, error) {
	if limit < 1 {
		return ReadResult{}, fmt.Errorf("session entry limit must be positive: %d", limit)
	}
	return read(path, offset, limit, false)
}

func read(path string, offset int64, limit int, keepTail bool) (ReadResult, error) {
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
		return ReadResult{}, &offsetBeyondError{offset: offset, size: st.Size()}
	}
	line, err := lineAt(f, offset)
	if err != nil {
		return ReadResult{}, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ReadResult{}, err
	}

	result := ReadResult{NextOffset: offset}
	ringNext := 0
	reader := bufio.NewReader(f)
	for {
		start := result.NextOffset
		raw, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return ReadResult{}, readErr
		}
		result.NextOffset += int64(len(raw))
		body := bytes.TrimSuffix(raw[:len(raw)-1], []byte{'\r'})
		entry, render := classify(body, line, start)
		if render {
			if keepTail && len(result.Entries) == limit {
				result.Entries[ringNext] = entry
				ringNext = (ringNext + 1) % limit
			} else if limit > 0 && len(result.Entries) == limit {
				result.NextOffset = start
				return result, nil
			} else {
				result.Entries = append(result.Entries, entry)
			}
		}
		line++
	}
	if keepTail && ringNext > 0 {
		ordered := make([]Entry, 0, len(result.Entries))
		ordered = append(ordered, result.Entries[ringNext:]...)
		ordered = append(ordered, result.Entries[:ringNext]...)
		result.Entries = ordered
	}
	return result, nil
}

// ReadTail returns the last limit renderable complete entries without keeping
// the full classified rollout in memory.
func ReadTail(path string, limit int) (ReadResult, int64, error) {
	if limit < 1 {
		return ReadResult{}, 0, fmt.Errorf("session entry limit must be positive: %d", limit)
	}
	result, err := read(path, 0, limit, true)
	if err != nil {
		return ReadResult{}, 0, err
	}
	from := result.NextOffset
	if len(result.Entries) > 0 {
		from = result.Entries[0].ByteOffset
	}
	return result, from, nil
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

func classify(raw []byte, line, offset int64) (Entry, bool) {
	base := Entry{Line: line, ByteOffset: offset, Kind: KindUnknown}
	var env rolloutEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		base.Payload = mustJSON(map[string]any{"raw": string(raw)})
		base.Quarantine = &Quarantine{Reason: "invalid_json"}
		return base, true
	}
	base.Timestamp = env.Timestamp
	switch env.Type {
	case "response_item":
		return classifyResponse(base, env.Payload, line)
	case "event_msg":
		return classifyEvent(base, env.Payload)
	case "compacted":
		base.Kind, base.Payload = KindCompactDivider, cloneRaw(env.Payload)
		return base, true
	case "session_meta", "turn_context", "world_state", "inter_agent_communication_metadata":
		return Entry{}, false
	default:
		base.Payload = cloneRaw(raw)
		base.Quarantine = &Quarantine{Reason: "unknown_type"}
		return base, true
	}
}

func classifyResponse(base Entry, raw json.RawMessage, line int64) (Entry, bool) {
	var item responsePayload
	if err := json.Unmarshal(raw, &item); err != nil {
		base.Payload = cloneRaw(raw)
		base.Quarantine = &Quarantine{Reason: "invalid_response_item"}
		return base, true
	}
	base.UUID = item.ID
	switch item.Type {
	case "message":
		return classifyMessage(base, item)
	case "reasoning":
		text := blockText(item.Summary, "summary_text")
		if text == "" {
			return Entry{}, false
		}
		base.Kind = KindThinking
		base.Payload = messagePayload("thinking", text, nil)
	case "function_call", "custom_tool_call":
		var input any
		if item.Type == "function_call" {
			input = decodeJSONArgument(item.Arguments)
		} else {
			input = normalizeCustomInput(item.Input)
		}
		base.Kind = KindToolUse
		base.Payload = mustJSON(map[string]any{"tool_use_id": item.CallID, "name": item.Name, "input": input})
	case "function_call_output", "custom_tool_call_output":
		base.Kind = KindToolResult
		base.Payload = normalizeToolResult(item.CallID, item.Output, item.Type == "custom_tool_call_output")
	case "tool_search_call":
		base.Kind = KindToolUse
		base.Payload = mustJSON(map[string]any{"tool_use_id": item.CallID, "name": "tool_search", "input": decodeJSONArgument(item.Arguments)})
	case "tool_search_output":
		base.Kind = KindToolResult
		base.Payload = normalizeToolResult(item.CallID, item.Tools, false)
	case "web_search_call":
		id := item.CallID
		if id == "" {
			id = fmt.Sprintf("codex-web-search-%d", line)
		}
		base.Kind = KindToolUse
		base.Payload = mustJSON(map[string]any{"tool_use_id": id, "name": "web_search", "input": rawJSONValue(item.Action)})
	case "agent_message":
		return Entry{}, false
	default:
		base.Payload = cloneRaw(raw)
		base.Quarantine = &Quarantine{Reason: "unknown_response_item"}
	}
	return base, true
}

func classifyMessage(base Entry, item responsePayload) (Entry, bool) {
	text := blockText(item.Content, "input_text", "output_text")
	switch item.Role {
	case "assistant":
		if text == "" {
			return Entry{}, false
		}
		base.Kind = KindAssistantText
		base.Payload = messagePayload("text", text, map[string]any{"phase": item.Phase})
		return base, true
	case "developer":
		trimmed := strings.TrimSpace(text)
		if !strings.HasPrefix(trimmed, "<hcom>") || !strings.HasSuffix(trimmed, "</hcom>") {
			return Entry{}, false
		}
		base.Kind = KindHcomDelivery
		base.Payload = normalizeHcomDelivery(trimmed)
		return base, true
	default:
		// User response items mirror event_msg/user_message, which is the
		// canonical prompt record and excludes session bootstrap injections.
		return Entry{}, false
	}
}

func classifyEvent(base Entry, raw json.RawMessage) (Entry, bool) {
	var event eventPayload
	if err := json.Unmarshal(raw, &event); err != nil {
		base.Payload = cloneRaw(raw)
		base.Quarantine = &Quarantine{Reason: "invalid_event_msg"}
		return base, true
	}
	switch event.Type {
	case "user_message":
		if strings.TrimSpace(event.Message) == "<hcom>" {
			base.Kind = KindHcomStub
			base.Payload = messagePayload("text", "<hcom>", nil)
		} else {
			base.Kind = KindHumanPrompt
			base.Payload = messagePayload("text", event.Message, nil)
		}
		return base, true
	case "task_complete":
		base.Kind = KindTurnDuration
		base.Payload = mustJSON(map[string]any{"durationMs": event.DurationMS, "messageCount": nil, "lastAgentMessage": event.LastAgentMessage})
		return base, true
	default:
		if _, known := knownEventTypes[event.Type]; known {
			return Entry{}, false
		}
		base.Payload = cloneRaw(raw)
		base.Quarantine = &Quarantine{Reason: "unknown_event_msg"}
		return base, true
	}
}

func messagePayload(blockType, text string, extra map[string]any) json.RawMessage {
	payload := map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": blockType, blockType: text}}}}
	for key, value := range extra {
		if value != "" && value != nil {
			payload[key] = value
		}
	}
	return mustJSON(payload)
}

func normalizeHcomDelivery(text string) json.RawMessage {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(text), "<hcom>"), "</hcom>"))
	matches := hcomMessagePattern.FindAllStringSubmatchIndex(body, -1)
	deliveries := make([]map[string]any, 0, max(len(matches), 1))
	for i, match := range matches {
		intent, thread, _ := strings.Cut(strings.TrimSpace(body[match[2]:match[3]]), ":")
		textEnd := len(body)
		if i+1 < len(matches) {
			textEnd = matches[i+1][0]
		}
		delivery := map[string]any{
			"intent": strings.TrimSpace(intent), "message_id": body[match[4]:match[5]],
			"sender": body[match[6]:match[7]], "recipient": strings.TrimSpace(body[match[8]:match[9]]),
			"text": strings.TrimSpace(body[match[1]:textEnd]),
		}
		if thread != "" {
			delivery["thread"] = strings.TrimSpace(thread)
		}
		deliveries = append(deliveries, delivery)
	}
	if len(deliveries) == 0 {
		deliveries = append(deliveries, map[string]any{"text": body})
	}
	return mustJSON(map[string]any{"subtype": "developer_message", "deliveries": deliveries})
}

func normalizeToolResult(callID string, raw json.RawMessage, custom bool) json.RawMessage {
	remaining, total, images, truncated := maxToolOutputBytes, 0, 0, false
	var normalized any
	var text string
	if json.Unmarshal(raw, &text) == nil {
		total = len([]byte(text))
		text, truncated = capText(text, remaining)
		normalized = text
	} else {
		var blocks []map[string]json.RawMessage
		if json.Unmarshal(raw, &blocks) == nil {
			out := make([]any, 0, len(blocks))
			for _, block := range blocks {
				typ := jsonString(block["type"])
				if typ == "image" || typ == "image_url" {
					images++
					out = append(out, map[string]any{"type": "image", "present": true})
					continue
				}
				value := jsonString(block["text"])
				if value == "" {
					encoded, _ := json.Marshal(block)
					value = string(encoded)
				}
				total += len([]byte(value))
				capped, cut := capText(value, remaining)
				remaining -= len([]byte(capped))
				truncated = truncated || cut
				out = append(out, map[string]any{"type": "text", "text": capped})
			}
			normalized = out
		} else {
			total = len(raw)
			value, cut := capText(string(raw), remaining)
			truncated, normalized = cut, value
		}
	}
	isError := false
	combined := resultText(normalized)
	if custom {
		isError = strings.HasPrefix(combined, "Script failed")
		if match := toolExitPattern.FindStringSubmatch(combined); len(match) == 2 {
			code, _ := strconv.Atoi(match[1])
			isError = isError || code != 0
		}
	} else if match := exitCodePattern.FindStringSubmatch(combined); len(match) == 2 {
		code, _ := strconv.Atoi(match[1])
		isError = code != 0
	}
	return mustJSON(map[string]any{
		"tool_use_id": callID, "is_error": isError, "content": normalized,
		"total_bytes": total, "truncated": truncated, "image_count": images,
	})
}

func resultText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var parts []string
		for _, raw := range typed {
			if block, ok := raw.(map[string]any); ok {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func blockText(raw json.RawMessage, allowed ...string) string {
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	allow := make(map[string]bool, len(allowed))
	for _, typ := range allowed {
		allow[typ] = true
	}
	var parts []string
	for _, block := range blocks {
		if allow[jsonString(block["type"])] {
			if value := jsonString(block["text"]); value != "" {
				parts = append(parts, value)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func decodeJSONArgument(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		var decoded any
		if json.Unmarshal([]byte(encoded), &decoded) == nil {
			return decoded
		}
		return encoded
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) == nil {
		return decoded
	}
	return string(raw)
}

func normalizeCustomInput(raw json.RawMessage) any {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return map[string]any{"source": value}
	}
	return rawJSONValue(raw)
}

func rawJSONValue(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	var value any
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return string(raw)
}

func capText(value string, limit int) (string, bool) {
	b := []byte(value)
	if len(b) <= limit {
		return value, false
	}
	end := min(max(limit, 0), len(b))
	floor := max(end-3, 0)
	for end > floor && end < len(b) && !utf8.RuneStart(b[end]) {
		end--
	}
	return string(b[:end]), true
}

func jsonString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
