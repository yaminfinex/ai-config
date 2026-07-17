package repaircmd

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/herdrcli"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestCrossPaneCeremonyRequiresStableVisibleNonceAndRemoval(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Operation: OperationRebind, GUID: "guid-live", Field: v2.BindingFieldSID, Value: "sid-new"}
	expected := attestationStatement("challenge-one", request)
	if _, err := writer.WriteString(expected + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	reads := []string{"challenge-one", "challenge-one", ""}
	collector := ProofCollector{
		OpenTTY: func() (*os.File, error) { return reader, nil }, IsTTY: func(*os.File) bool { return true },
		PaneGet: func(context.Context, string) (herdrcli.Pane, error) {
			return herdrcli.Pane{PaneID: "pane-live", TerminalID: "terminal-live"}, nil
		},
		ReadVisible:  func(context.Context, string) (string, error) { out := reads[0]; reads = reads[1:]; return out, nil },
		NewChallenge: func() (string, error) { return "challenge-one", nil },
		Now:          time.Now, Wait: func(time.Duration) {},
	}
	proof, err := collector.Collect(context.Background(), v2.SessionRecord{Seat: &v2.Seat{PaneID: "pane-live", TerminalID: "terminal-live"}}, request)
	if err != nil || proof.Statement != expected {
		t.Fatalf("Collect proof=%+v err=%v", proof, err)
	}
}

func TestCrossPaneCeremonyRefusesIntactTerminalMismatch(t *testing.T) {
	reader, _, _ := os.Pipe()
	collector := ProofCollector{
		OpenTTY: func() (*os.File, error) { return reader, nil }, IsTTY: func(*os.File) bool { return true },
		PaneGet: func(context.Context, string) (herdrcli.Pane, error) {
			return herdrcli.Pane{PaneID: "pane-live", TerminalID: "terminal-other"}, nil
		},
	}
	_, err := collector.Collect(context.Background(), v2.SessionRecord{Seat: &v2.Seat{PaneID: "pane-live", TerminalID: "terminal-recorded"}}, Request{Operation: OperationReissueCredential, GUID: "guid-live"})
	if !errors.Is(err, ErrCorroborationFailed) {
		t.Fatalf("terminal mismatch err = %v", err)
	}
}

func TestCrossPaneCeremonyRefusesNonceConsumedBetweenVisibleReads(t *testing.T) {
	reader, _, _ := os.Pipe()
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	reads := 0
	collector := ProofCollector{
		OpenTTY: func() (*os.File, error) { return reader, nil }, IsTTY: func(*os.File) bool { return true },
		PaneGet: func(context.Context, string) (herdrcli.Pane, error) {
			return herdrcli.Pane{PaneID: "pane-live", TerminalID: "terminal-live"}, nil
		},
		ReadVisible: func(context.Context, string) (string, error) {
			reads++
			if reads%2 == 1 {
				return "challenge-one", nil
			}
			return "redrawn", nil
		},
		NewChallenge: func() (string, error) { return "challenge-one", nil },
		Now:          func() time.Time { return now }, Wait: func(time.Duration) { now = now.Add(31 * time.Second) },
	}
	_, err := collector.Collect(context.Background(), v2.SessionRecord{Seat: &v2.Seat{PaneID: "pane-live", TerminalID: "terminal-live"}}, Request{Operation: OperationReissueCredential, GUID: "guid-live"})
	if err == nil || !strings.Contains(err.Error(), "two consecutive") {
		t.Fatalf("unstable nonce err = %v", err)
	}
}
