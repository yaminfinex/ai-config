// Package hcomtranscript reads bounded transcript windows through hcom's
// supported transcript command. It never opens transcript files itself.
package hcomtranscript

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"ai-config/tools/herder/internal/hcomcli"
)

const queryTimeout = 10 * time.Second

type Detail string

const (
	Exchanges Detail = "exchanges"
	Full      Detail = "full"
)

// Exchange preserves hcom's JSON exchange shape while exposing its stable
// one-based position for paging and tail cursors.
type Exchange struct {
	Position int
	raw      json.RawMessage
}

func (e *Exchange) UnmarshalJSON(raw []byte) error {
	var header struct {
		Position int `json:"position"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	if header.Position < 1 {
		return errors.New("exchange has no positive position")
	}
	e.Position = header.Position
	e.raw = append(e.raw[:0], raw...)
	return nil
}

func (e Exchange) MarshalJSON() ([]byte, error) {
	if len(e.raw) == 0 {
		return nil, errors.New("exchange has no JSON payload")
	}
	return e.raw, nil
}

// Window reads at most limit exchanges before the exclusive position. A zero
// before position means the newest limit exchanges.
func Window(ctx context.Context, agent string, before, limit int, detail Detail) ([]Exchange, error) {
	if limit < 1 {
		return nil, errors.New("limit must be positive")
	}
	if before == 1 {
		return []Exchange{}, nil
	}
	if before < 0 {
		return nil, errors.New("before position must not be negative")
	}
	if before == 0 {
		return run(ctx, agent, []string{"--last", strconv.Itoa(limit)}, detail, 0, 0, limit)
	}
	end := before - 1
	start := end - limit + 1
	if start < 1 {
		start = 1
	}
	return Range(ctx, agent, start, end, detail)
}

// Latest reads only the newest exchange and returns zero for an empty result.
func Latest(ctx context.Context, agent string, detail Detail) (Exchange, bool, error) {
	exchanges, err := run(ctx, agent, []string{"--last", "1"}, detail, 0, 0, 1)
	if err != nil || len(exchanges) == 0 {
		return Exchange{}, false, err
	}
	return exchanges[0], true, nil
}

// Range reads an inclusive, bounded exchange-position range.
func Range(ctx context.Context, agent string, start, end int, detail Detail) ([]Exchange, error) {
	if start < 1 || end < start {
		return nil, fmt.Errorf("invalid transcript range %d-%d", start, end)
	}
	return run(ctx, agent, []string{fmt.Sprintf("%d-%d", start, end)}, detail, start, end, end-start+1)
}

func run(ctx context.Context, agent string, selector []string, detail Detail, start, end, limit int) ([]Exchange, error) {
	if detail != Exchanges && detail != Full {
		return nil, fmt.Errorf("unknown transcript detail %q", detail)
	}
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	args := append([]string{"transcript", agent}, selector...)
	if detail == Full {
		args = append(args, "--detailed")
	}
	args = append(args, "--json")
	cmd := hcomcli.CommandContext(queryCtx, args...)
	out, err := cmd.Output()
	if err != nil {
		if queryCtx.Err() != nil {
			return nil, fmt.Errorf("hcom transcript timed out: %w", queryCtx.Err())
		}
		return nil, fmt.Errorf("hcom transcript failed: %s", commandError(err))
	}
	decoder := json.NewDecoder(bytes.NewReader(out))
	var exchanges []Exchange
	if err := decoder.Decode(&exchanges); err != nil {
		return nil, fmt.Errorf("invalid hcom transcript JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid hcom transcript JSON: trailing content")
	}
	if exchanges == nil {
		exchanges = []Exchange{}
	}
	if len(exchanges) > limit {
		return nil, fmt.Errorf("hcom transcript returned %d exchanges for a %d-exchange window", len(exchanges), limit)
	}
	previous := 0
	for index, exchange := range exchanges {
		if exchange.Position <= previous {
			return nil, errors.New("hcom transcript positions are not newest-last")
		}
		if start != 0 && (exchange.Position < start || exchange.Position > end) {
			return nil, fmt.Errorf("hcom transcript returned position %d outside requested range %d-%d", exchange.Position, start, end)
		}
		if start != 0 && exchange.Position != start+index {
			return nil, fmt.Errorf("hcom transcript omitted position %d from requested range %d-%d", start+index, start, end)
		}
		previous = exchange.Position
	}
	if start != 0 && len(exchanges) != limit {
		return nil, fmt.Errorf("hcom transcript returned %d exchanges for requested range %d-%d", len(exchanges), start, end)
	}
	return exchanges, nil
}

func commandError(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(bytes.TrimSpace(exitErr.Stderr)) > 0 {
		return string(bytes.TrimSpace(exitErr.Stderr))
	}
	return strings.TrimSpace(err.Error())
}
