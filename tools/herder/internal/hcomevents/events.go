// Package hcomevents consumes the hcom bus through its supported CLI event
// wait mechanism. It does not read hcom's database or retain a cursor beyond
// one client connection.
package hcomevents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	ID     int64    `json:"id"`
	From   string   `json:"from"`
	To     []string `json:"to"`
	Intent string   `json:"intent,omitempty"`
	Thread string   `json:"thread"`
	Text   string   `json:"text"`
	SentAt string   `json:"sent_at,omitempty"`
}

// DeliveryWatermark is the recipient cursor recorded by hcom after a delivery
// batch. Position is the last bus event consumed by that concrete recipient;
// MessageTimestamp identifies the batch tail and is corroboration only.
type DeliveryWatermark struct {
	Recipient        string
	Position         int64
	MessageTimestamp string
}

const catchUpTimeout = 5 * time.Second

type Cursor struct {
	ID          int64
	initialized bool
}

type event struct {
	ID       json.Number `json:"id"`
	TS       string      `json:"ts"`
	Type     string      `json:"type"`
	Instance string      `json:"instance"`
	Data     struct {
		From        string      `json:"from"`
		DeliveredTo []string    `json:"delivered_to"`
		Mentions    []string    `json:"mentions"`
		Intent      string      `json:"intent"`
		Thread      string      `json:"thread"`
		Text        string      `json:"text"`
		Context     string      `json:"context"`
		Position    json.Number `json:"position"`
		MsgTS       string      `json:"msg_ts"`
	} `json:"data"`
	TimedOut bool `json:"timed_out"`
}

// LatestDelivery returns the newest delivery cursor recorded for recipient.
// An empty result is a valid fact: callers may retain their transcript-only
// evidence. Query failures remain distinguishable so optional queue facts can
// be omitted instead of guessed.
func LatestDelivery(ctx context.Context, recipient string) (DeliveryWatermark, bool, error) {
	if recipient == "" {
		return DeliveryWatermark{}, false, errors.New("empty hcom delivery recipient")
	}
	cmd := command(ctx, "events", "--last", "1", "--full", "--type", "status", "--agent", recipient, "--context", "deliver:*")
	out, err := cmd.Output()
	if err != nil {
		return DeliveryWatermark{}, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(out))
	decoder.UseNumber()
	var parsed event
	if err := decoder.Decode(&parsed); errors.Is(err, io.EOF) {
		return DeliveryWatermark{}, false, nil
	} else if err != nil {
		return DeliveryWatermark{}, false, err
	}
	watermark, err := projectDelivery(parsed)
	if err != nil {
		return DeliveryWatermark{}, false, err
	}
	return watermark, true, nil
}

func projectDelivery(parsed event) (DeliveryWatermark, error) {
	if parsed.Type != "status" || parsed.Instance == "" || !strings.HasPrefix(parsed.Data.Context, "deliver:") {
		return DeliveryWatermark{}, fmt.Errorf("invalid hcom delivery event %q for %q", parsed.Type, parsed.Instance)
	}
	position, err := strconv.ParseInt(parsed.Data.Position.String(), 10, 64)
	if err != nil || position < 0 {
		return DeliveryWatermark{}, fmt.Errorf("invalid hcom delivery position %q", parsed.Data.Position)
	}
	return DeliveryWatermark{
		Recipient: parsed.Instance, Position: position, MessageTimestamp: parsed.Data.MsgTS,
	}, nil
}

// Recent returns the most recent bus message events in bus order. It uses the
// same event decoder and projection as Subscribe so endpoint reads and stream
// wake frames cannot drift in shape.
func Recent(ctx context.Context, limit int) ([]Message, error) {
	events, err := query(ctx, limit, -1)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, 0, len(events))
	for _, parsed := range events {
		if parsed.Type != "message" {
			continue
		}
		message, err := projectMessage(parsed)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// Subscribe blocks, forwarding every new bus message until ctx is canceled.
// The blocking --wait query is hcom's own event subscription/wakeup path.
func Subscribe(ctx context.Context, cursor *Cursor, emit func(Message) error, healthy func() error) error {
	if !cursor.initialized {
		events, err := query(ctx, 1, -1)
		if err != nil {
			return fmt.Errorf("hcom events subscription baseline failed: %w", err)
		}
		for _, event := range events {
			id, err := eventID(event)
			if err != nil {
				return err
			}
			if id > cursor.ID {
				cursor.ID = id
			}
		}
		cursor.initialized = true
		if err := healthy(); err != nil {
			return err
		}
	}
	for {
		filter := fmt.Sprintf("id > %d", cursor.ID)
		wake, err := run(ctx, "events", "--wait", "30", "--full", "--type", "message", "--sql", filter)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("hcom events subscription failed: %w", err)
		}
		if err := healthy(); err != nil {
			return err
		}
		if wake.TimedOut {
			continue
		}

		// Wait mode is the subscription/wakeup mechanism, but it returns only
		// one matching event. Query the full window after the old cursor so a
		// burst between wakeups is forwarded without dropping earlier messages.
		queryCtx, cancel := context.WithTimeout(ctx, catchUpTimeout)
		events, err := query(queryCtx, 10000, cursor.ID)
		cancel()
		if err != nil {
			return fmt.Errorf("hcom events subscription catch-up failed: %w", err)
		}
		progressed := false
		for _, parsed := range events {
			if parsed.Type != "message" {
				continue
			}
			message, err := projectMessage(parsed)
			if err != nil {
				return err
			}
			if message.ID <= cursor.ID {
				continue
			}
			if err := emit(message); err != nil {
				return err
			}
			cursor.ID = message.ID
			progressed = true
		}
		if !progressed {
			timer := time.NewTimer(time.Second)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil
			}
		}
	}
}

func projectMessage(parsed event) (Message, error) {
	id, err := eventID(parsed)
	if err != nil {
		return Message{}, err
	}
	to := parsed.Data.DeliveredTo
	if len(to) == 0 {
		to = parsed.Data.Mentions
	}
	if to == nil {
		to = []string{}
	}
	return Message{
		ID: id, From: parsed.Data.From, To: to, Intent: parsed.Data.Intent,
		Thread: parsed.Data.Thread, Text: parsed.Data.Text, SentAt: parsed.TS,
	}, nil
}

func eventID(parsed event) (int64, error) {
	id, err := strconv.ParseInt(parsed.ID.String(), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid hcom event id %q: %w", parsed.ID, err)
	}
	return id, nil
}

func run(ctx context.Context, args ...string) (event, error) {
	cmd := command(ctx, args...)
	out, err := cmd.Output()
	parsed, parseErr := decode(out)
	if parseErr == nil && parsed.TimedOut {
		return parsed, nil
	}
	if err != nil {
		detail := err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(bytes.TrimSpace(exitErr.Stderr)) > 0 {
			detail = string(bytes.TrimSpace(exitErr.Stderr))
		}
		return event{}, errors.New(detail)
	}
	if parseErr != nil {
		return event{}, fmt.Errorf("invalid JSON: %w", parseErr)
	}
	return parsed, nil
}

func query(ctx context.Context, limit int, afterID int64) ([]event, error) {
	args := []string{"events", "--last", strconv.Itoa(limit), "--full", "--type", "message"}
	if afterID >= 0 {
		args = append(args, "--sql", fmt.Sprintf("id > %d", afterID))
	}
	cmd := command(ctx, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(out))
	decoder.UseNumber()
	var events []event
	for {
		var parsed event
		if err := decoder.Decode(&parsed); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		events = append(events, parsed)
	}
	return events, nil
}

func command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "hcom", args...)
	// A server started from an agent pane inherits that agent's hcom identity.
	// Anonymous event waits must not register as that agent or be interrupted
	// by its unread-message notifications. Preserve only the bus directory.
	cmd.Env = make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "HCOM_") && name != "HCOM_DIR" {
			continue
		}
		switch name {
		case "HERDR_PANE_ID", "HERDR_TAB_ID", "HERDR_WORKSPACE_ID":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	return cmd
}

func decode(raw []byte) (event, error) {
	var result event
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return event{}, err
	}
	return result, nil
}
