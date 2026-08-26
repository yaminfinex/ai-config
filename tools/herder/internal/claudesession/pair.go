package claudesession

import "encoding/json"

// Pair returns a pure consumer-side view. It never changes, removes, or
// folds the input entries. Related entries are still emitted separately.
func Pair(entries []Entry) []PairedEntry {
	resultByID := make(map[string]int)
	for i := range entries {
		if entries[i].Kind == KindToolResult {
			if id := payloadString(entries[i].Payload, "tool_use_id"); id != "" {
				resultByID[id] = i
			}
		}
	}
	out := make([]PairedEntry, len(entries))
	for i := range entries {
		out[i].Primary = entries[i]
		switch entries[i].Kind {
		case KindToolUse:
			if j, ok := resultByID[payloadString(entries[i].Payload, "tool_use_id")]; ok {
				related := entries[j]
				out[i].Related = &related
			}
		case KindHcomStub:
			if i+1 < len(entries) && entries[i+1].Kind == KindHcomDelivery {
				related := entries[i+1]
				out[i].Related = &related
			}
		}
	}
	return out
}

func payloadString(raw json.RawMessage, key string) string {
	var payload map[string]json.RawMessage
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return stringValue(payload, key)
}
