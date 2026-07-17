package repaircmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

const (
	ceremonyTimeout       = 90 * time.Second
	minimumChallengeBytes = 16
)

type ProofCollector struct {
	Stderr              io.Writer
	OpenTTY             func() (*os.File, error)
	IsTTY               func(*os.File) bool
	PaneGet             func(context.Context, string) (herdrcli.Pane, error)
	ReadVisible         func(context.Context, string) (string, error)
	NewChallenge        func() (string, error)
	Now                 func() time.Time
	Wait                func(time.Duration)
	ConfirmationTimeout time.Duration
}

func DefaultProofCollector(stderr io.Writer) ProofCollector {
	return ProofCollector{
		Stderr:  stderr,
		OpenTTY: func() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) },
		IsTTY:   func(f *os.File) bool { return isatty.IsTerminal(f.Fd()) },
		PaneGet: func(_ context.Context, paneID string) (herdrcli.Pane, error) {
			out, err := (&herdrcli.Client{}).Output("pane", "get", paneID)
			if err != nil {
				return herdrcli.Pane{}, err
			}
			return herdrcli.ParsePaneGet(out)
		},
		ReadVisible: func(ctx context.Context, paneID string) (string, error) {
			cmd := exec.CommandContext(ctx, "herdr", "pane", "read", paneID, "--source", "visible", "--lines", "120")
			out, err := cmd.Output()
			return string(out), err
		},
		NewChallenge:        registry.NewGUID,
		Now:                 time.Now,
		Wait:                time.Sleep,
		ConfirmationTimeout: ceremonyTimeout,
	}
}

func (c ProofCollector) Collect(ctx context.Context, rec v2.SessionRecord, request Request) (Proof, error) {
	if rec.Seat == nil {
		return Proof{}, ErrCorroborationFailed
	}
	tty, err := c.OpenTTY()
	if err != nil {
		return Proof{}, fmt.Errorf("%w: /dev/tty unavailable", ErrAttestationRequired)
	}
	defer tty.Close()
	isTTY := c.IsTTY
	if isTTY == nil {
		isTTY = func(f *os.File) bool { return isatty.IsTerminal(f.Fd()) }
	}
	if !isTTY(tty) {
		return Proof{}, fmt.Errorf("%w: controlling device is not a tty", ErrAttestationRequired)
	}
	live, err := c.PaneGet(ctx, rec.Seat.PaneID)
	if err != nil || live.PaneID != rec.Seat.PaneID || (rec.Seat.TerminalID != "" && live.TerminalID != rec.Seat.TerminalID) {
		return Proof{}, fmt.Errorf("%w: claimed pane or intact terminal id does not match live herdr state", ErrCorroborationFailed)
	}
	challenge, err := c.NewChallenge()
	if err != nil {
		return Proof{}, err
	}
	if len(challenge) < minimumChallengeBytes {
		return Proof{}, fmt.Errorf("%w: generated challenge is too short", ErrCorroborationFailed)
	}
	expected := attestationStatement(challenge, request)
	stderr := c.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	fmt.Fprintf(stderr, "herder repair: BREAK-GLASS challenge %s\n", challenge)
	fmt.Fprintf(stderr, "Target pane %s is read-only from this command. Place the challenge verbatim in that pane's VISIBLE composer WITHOUT pressing Enter. Do not submit it and do not destroy an existing draft.\n", rec.Seat.PaneID)
	deadline := c.Now().Add(ceremonyTimeout)
	stableReads := 0
	for c.Now().Before(deadline) {
		pane, paneErr := c.PaneGet(ctx, rec.Seat.PaneID)
		visible, readErr := c.ReadVisible(ctx, rec.Seat.PaneID)
		if paneErr != nil || readErr != nil || pane.PaneID != rec.Seat.PaneID || (rec.Seat.TerminalID != "" && pane.TerminalID != rec.Seat.TerminalID) {
			return Proof{}, fmt.Errorf("%w: pane changed or became unreadable during challenge", ErrCorroborationFailed)
		}
		if strings.Contains(visible, challenge) {
			stableReads++
			if stableReads == 2 {
				break
			}
		} else {
			stableReads = 0
		}
		c.Wait(250 * time.Millisecond)
	}
	if stableReads < 2 {
		return Proof{}, fmt.Errorf("%w: challenge was not found verbatim in two consecutive visible-pane reads", ErrCorroborationFailed)
	}
	fmt.Fprintln(stderr, "Challenge observed twice. Remove it from the target composer WITHOUT pressing Enter, return here, then type the exact attestation below.")
	fmt.Fprintln(stderr, expected)
	line, err := readConfirmation(ctx, tty, c.ConfirmationTimeout)
	if err != nil {
		return Proof{}, err
	}
	if strings.TrimSpace(line) != expected {
		return Proof{}, fmt.Errorf("%w: confirmation did not match", ErrAttestationRequired)
	}
	visible, err := c.ReadVisible(ctx, rec.Seat.PaneID)
	if err != nil || strings.Contains(visible, challenge) {
		return Proof{}, fmt.Errorf("%w: challenge remains visible; clear it without submitting and retry", ErrCorroborationFailed)
	}
	live, err = c.PaneGet(ctx, rec.Seat.PaneID)
	if err != nil || live.PaneID != rec.Seat.PaneID || (rec.Seat.TerminalID != "" && live.TerminalID != rec.Seat.TerminalID) {
		return Proof{}, fmt.Errorf("%w: pane identity changed before commit", ErrCorroborationFailed)
	}
	return Proof{Statement: expected, PaneID: live.PaneID, TerminalID: live.TerminalID}, nil
}

type confirmationRead struct {
	line string
	err  error
}

func readConfirmation(ctx context.Context, tty *os.File, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = ceremonyTimeout
	}
	result := make(chan confirmationRead, 1)
	go func() {
		line, err := bufio.NewReader(tty).ReadString('\n')
		result <- confirmationRead{line: line, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		_ = tty.Close()
		return "", fmt.Errorf("%w: confirmation canceled: %v", ErrAttestationRequired, ctx.Err())
	case <-timer.C:
		_ = tty.Close()
		return "", fmt.Errorf("%w: confirmation timed out after %s", ErrAttestationRequired, timeout)
	case read := <-result:
		if read.err != nil {
			return "", fmt.Errorf("%w: confirmation was not read", ErrAttestationRequired)
		}
		return read.line, nil
	}
}

func attestationStatement(challenge string, request Request) string {
	field, value := request.Field, request.Value
	if field == "" {
		field = "-"
	}
	if value == "" {
		value = "-"
	}
	return fmt.Sprintf("ATTEST %s REPAIR %s %s %s %s", challenge, request.GUID, request.Operation, field, value)
}
