package servecmd

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomevents"
)

const queuedPreviewRunes = 240

func candidateMessageTimes(agent string, messages []hcomevents.Message) map[string]string {
	candidates := make(map[string]string)
	for _, message := range messages {
		if slices.Contains(message.To, agent) {
			candidates[strconv.FormatInt(message.ID, 10)] = message.SentAt
		}
	}
	return candidates
}

func diffQueuedMessages(agent string, messages []hcomevents.Message, excluded map[string]bool) []queuedMessage {
	queued := make([]queuedMessage, 0)
	for _, message := range messages {
		if !slices.Contains(message.To, agent) || excluded[strconv.FormatInt(message.ID, 10)] {
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

type queueProof struct {
	excluded             map[string]bool
	honorCompactBoundary bool
	latestCompact        time.Time
}

func newQueueProof(honorCompactBoundary bool) *queueProof {
	return &queueProof{
		excluded:             make(map[string]bool),
		honorCompactBoundary: honorCompactBoundary,
	}
}

func (proof *queueProof) observe(entries []claudesession.Entry, candidates map[string]string) error {
	for _, entry := range entries {
		switch entry.Kind {
		case claudesession.KindHcomDelivery:
			var payload struct {
				Deliveries []struct {
					MessageID string `json:"message_id"`
				} `json:"deliveries"`
			}
			if json.Unmarshal(entry.Payload, &payload) != nil {
				continue
			}
			for _, delivery := range payload.Deliveries {
				if _, wanted := candidates[delivery.MessageID]; wanted {
					proof.excluded[delivery.MessageID] = true
				}
			}
		case claudesession.KindCompactDivider:
			if !proof.honorCompactBoundary {
				continue
			}
			boundary, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
			if err != nil {
				return fmt.Errorf("invalid compact boundary timestamp %q: %w", entry.Timestamp, err)
			}
			if boundary.After(proof.latestCompact) {
				proof.latestCompact = boundary
			}
		}
	}
	return nil
}

func (proof *queueProof) exclusions(candidates map[string]string) (map[string]bool, error) {
	if proof.latestCompact.IsZero() {
		return proof.excluded, nil
	}
	for id, rawSentAt := range candidates {
		if proof.excluded[id] {
			continue
		}
		sentAt, err := time.Parse(time.RFC3339Nano, rawSentAt)
		if err != nil {
			return nil, fmt.Errorf("invalid queued message timestamp %q: %w", rawSentAt, err)
		}
		if sentAt.Before(proof.latestCompact) {
			proof.excluded[id] = true
		}
	}
	return proof.excluded, nil
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
