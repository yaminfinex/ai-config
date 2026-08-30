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
	"ai-config/tools/herder/internal/hcomidentity"
)

const queuedPreviewRunes = 240

type queueCandidate struct {
	SentAt        string
	Sender        string
	Recipient     string
	RecipientBase string
	Intent        string
	Thread        string
	Preview       string
}

func operatorQueueCandidates(agent, baseName string, messages []hcomevents.Message, roster []hcomidentity.Row) map[string]queueCandidate {
	candidates := make(map[string]queueCandidate)
	for _, message := range messages {
		if !slices.Contains(message.To, agent) && (baseName == "" || !slices.Contains(message.To, baseName)) {
			continue
		}
		preview, operator := queuedPresentation(message.Text)
		if operator {
			candidates[strconv.FormatInt(message.ID, 10)] = queueCandidate{
				SentAt: message.SentAt, Sender: queueSenderName(message.From, roster), Recipient: agent, RecipientBase: baseName,
				Intent: message.Intent, Thread: message.Thread, Preview: preview,
			}
		}
	}
	return candidates
}

func excludeDeliveredCandidates(candidates map[string]queueCandidate, excluded map[string]bool, watermark hcomevents.DeliveryWatermark) map[string]bool {
	if excluded == nil {
		excluded = make(map[string]bool)
	}
	// Safe only because hcom advances a name-keyed cursor after successful injection, including mentions fallback; advancing past an undelivered mention would over-clear.
	for id, candidate := range candidates {
		if watermark.Recipient != candidate.Recipient && (candidate.RecipientBase == "" || watermark.Recipient != candidate.RecipientBase) {
			continue
		}
		messageID, err := strconv.ParseInt(id, 10, 64)
		if err == nil && messageID <= watermark.Position {
			excluded[id] = true
		}
	}
	return excluded
}

func queueSenderName(sender string, roster []hcomidentity.Row) string {
	resolved := ""
	for _, row := range roster {
		if row.Name != sender && row.BaseName != sender {
			continue
		}
		if resolved != "" && resolved != row.Name {
			return sender
		}
		resolved = row.Name
	}
	if resolved != "" {
		return resolved
	}
	return sender
}

func diffQueuedMessages(messages []hcomevents.Message, candidates map[string]queueCandidate, excluded map[string]bool) []queuedMessage {
	queued := make([]queuedMessage, 0)
	for _, message := range messages {
		id := strconv.FormatInt(message.ID, 10)
		candidate, wanted := candidates[id]
		if !wanted || excluded[id] {
			continue
		}
		queued = append(queued, queuedMessage{
			ID: message.ID, Sender: message.From, Intent: message.Intent,
			Preview: candidate.Preview, SentAt: message.SentAt, Operator: true,
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

func (proof *queueProof) observe(entries []claudesession.Entry, candidates map[string]queueCandidate) error {
	for _, entry := range entries {
		switch entry.Kind {
		case claudesession.KindHcomDelivery:
			var payload struct {
				Deliveries []struct {
					MessageID string `json:"message_id"`
					Sender    string `json:"sender"`
					Recipient string `json:"recipient"`
					Intent    string `json:"intent"`
					Thread    string `json:"thread"`
				} `json:"deliveries"`
			}
			if json.Unmarshal(entry.Payload, &payload) != nil {
				continue
			}
			for _, delivery := range payload.Deliveries {
				candidate, wanted := candidates[delivery.MessageID]
				recipientMatches := delivery.Recipient == candidate.Recipient || candidate.RecipientBase != "" && delivery.Recipient == candidate.RecipientBase
				if wanted && delivery.Sender == candidate.Sender && recipientMatches && delivery.Intent == candidate.Intent && delivery.Thread == candidate.Thread {
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

func (proof *queueProof) exclusions(candidates map[string]queueCandidate) (map[string]bool, error) {
	if proof.latestCompact.IsZero() {
		return proof.excluded, nil
	}
	for id, candidate := range candidates {
		if proof.excluded[id] {
			continue
		}
		sentAt, err := time.Parse(time.RFC3339Nano, candidate.SentAt)
		if err != nil {
			return nil, fmt.Errorf("invalid queued message timestamp %q: %w", candidate.SentAt, err)
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
