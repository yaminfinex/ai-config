package servecmd

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomevents"
)

const queuedPreviewRunes = 240

func candidateMessageIDs(agent string, messages []hcomevents.Message) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, message := range messages {
		if slices.Contains(message.To, agent) {
			ids[strconv.FormatInt(message.ID, 10)] = struct{}{}
		}
	}
	return ids
}

func diffQueuedMessages(agent string, messages []hcomevents.Message, delivered map[string]bool) []queuedMessage {
	queued := make([]queuedMessage, 0)
	for _, message := range messages {
		if !slices.Contains(message.To, agent) || delivered[strconv.FormatInt(message.ID, 10)] {
			continue
		}
		preview, operator := queuedPresentation(message.Text)
		queued = append(queued, queuedMessage{
			ID: message.ID, Sender: message.From, Intent: message.Intent,
			Preview: preview, SentAt: message.SentAt, Operator: operator,
		})
	}
	return queued
}

func deliveredMessageIDs(entries []claudesession.Entry) map[string]bool {
	delivered := make(map[string]bool)
	for _, entry := range entries {
		if entry.Kind != claudesession.KindHcomDelivery {
			continue
		}
		var payload struct {
			Deliveries []struct {
				MessageID string `json:"message_id"`
			} `json:"deliveries"`
		}
		if json.Unmarshal(entry.Payload, &payload) != nil {
			continue
		}
		for _, delivery := range payload.Deliveries {
			if delivery.MessageID != "" {
				delivered[delivery.MessageID] = true
			}
		}
	}
	return delivered
}

func queuedPresentation(text string) (string, bool) {
	operator := false
	if strings.HasPrefix(text, webNoteStart+"\n") {
		if end := strings.Index(text, "\n"+webNoteEnd+"\n\n"); end >= 0 {
			text = text[end+len("\n"+webNoteEnd+"\n\n"):]
			operator = true
		}
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= queuedPreviewRunes {
		return text, operator
	}
	runes := []rune(text)
	return string(runes[:queuedPreviewRunes-1]) + "…", operator
}
