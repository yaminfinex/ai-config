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
	challenge := "challenge-one-1234"
	expected := attestationStatement(challenge, request)
	if _, err := writer.WriteString(expected + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	reads := []string{challenge, challenge, ""}
	collector := ProofCollector{
		OpenTTY: func() (*os.File, error) { return reader, nil }, IsTTY: func(*os.File) bool { return true },
		PaneGet: func(context.Context, string) (herdrcli.Pane, error) {
			return herdrcli.Pane{PaneID: "pane-live", TerminalID: "terminal-live"}, nil
		},
		ReadVisible:  func(context.Context, string) (string, error) { out := reads[0]; reads = reads[1:]; return out, nil },
		NewChallenge: func() (string, error) { return challenge, nil },
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
	challenge := "challenge-one-1234"
	reads := 0
	collector := ProofCollector{
		OpenTTY: func() (*os.File, error) { return reader, nil }, IsTTY: func(*os.File) bool { return true },
		PaneGet: func(context.Context, string) (herdrcli.Pane, error) {
			return herdrcli.Pane{PaneID: "pane-live", TerminalID: "terminal-live"}, nil
		},
		ReadVisible: func(context.Context, string) (string, error) {
			reads++
			if reads%2 == 1 {
				return challenge, nil
			}
			return "redrawn", nil
		},
		NewChallenge: func() (string, error) { return challenge, nil },
		Now:          func() time.Time { return now }, Wait: func(time.Duration) { now = now.Add(31 * time.Second) },
	}
	_, err := collector.Collect(context.Background(), v2.SessionRecord{Seat: &v2.Seat{PaneID: "pane-live", TerminalID: "terminal-live"}}, Request{Operation: OperationReissueCredential, GUID: "guid-live"})
	if err == nil || !strings.Contains(err.Error(), "two consecutive") {
		t.Fatalf("unstable nonce err = %v", err)
	}
}

func TestCrossPaneCeremonyRefusesChallengeStillVisibleAfterConfirmation(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	challenge := "challenge-still-visible"
	if _, err := writer.WriteString(attestationStatement(challenge, ceremonyRequest()) + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	collector := confirmationCollector(reader, challenge)
	collector.ReadVisible = func(context.Context, string) (string, error) { return challenge, nil }
	_, err = collector.Collect(context.Background(), ceremonyRecord(), ceremonyRequest())
	if !errors.Is(err, ErrCorroborationFailed) || !strings.Contains(err.Error(), "challenge remains visible") {
		t.Fatalf("visible challenge err = %v", err)
	}
}

func TestCrossPaneCeremonyBoundsTTYConfirmation(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	collector := confirmationCollector(reader, "challenge-timeout-1234")
	collector.ConfirmationTimeout = 20 * time.Millisecond
	started := time.Now()
	_, err = collector.Collect(context.Background(), ceremonyRecord(), ceremonyRequest())
	if !errors.Is(err, ErrAttestationRequired) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("confirmation timeout err = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("confirmation timeout took %s", elapsed)
	}
}

func TestCrossPaneCeremonyRefusesTTYEOF(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	collector := confirmationCollector(reader, "challenge-eof-123456")
	_, err = collector.Collect(context.Background(), ceremonyRecord(), ceremonyRequest())
	if !errors.Is(err, ErrAttestationRequired) || !strings.Contains(err.Error(), "not read") {
		t.Fatalf("confirmation EOF err = %v", err)
	}
}

func TestCrossPaneCeremonyRefusesTTYMismatch(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString("wrong confirmation\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	collector := confirmationCollector(reader, "challenge-mismatch-1")
	_, err = collector.Collect(context.Background(), ceremonyRecord(), ceremonyRequest())
	if !errors.Is(err, ErrAttestationRequired) || !strings.Contains(err.Error(), "did not match") {
		t.Fatalf("confirmation mismatch err = %v", err)
	}
}

func TestCrossPaneCeremonyRefusesEmptyOrShortChallengeAtSource(t *testing.T) {
	for _, challenge := range []string{"", "short"} {
		t.Run(challenge, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			expected := attestationStatement(challenge, ceremonyRequest())
			if _, err := writer.WriteString(expected + "\n"); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			collector := confirmationCollector(reader, challenge)
			_, err = collector.Collect(context.Background(), ceremonyRecord(), ceremonyRequest())
			if !errors.Is(err, ErrCorroborationFailed) || !strings.Contains(err.Error(), "challenge is too short") {
				t.Fatalf("short challenge %q err = %v", challenge, err)
			}
		})
	}
}

func confirmationCollector(reader *os.File, challenge string) ProofCollector {
	reads := []string{challenge, challenge, ""}
	return ProofCollector{
		OpenTTY: func() (*os.File, error) { return reader, nil }, IsTTY: func(*os.File) bool { return true },
		PaneGet: func(context.Context, string) (herdrcli.Pane, error) {
			return herdrcli.Pane{PaneID: "pane-live", TerminalID: "terminal-live"}, nil
		},
		ReadVisible: func(context.Context, string) (string, error) {
			out := reads[0]
			reads = reads[1:]
			return out, nil
		},
		NewChallenge: func() (string, error) { return challenge, nil },
		Now:          time.Now,
		Wait:         func(time.Duration) {},
	}
}

func ceremonyRecord() v2.SessionRecord {
	return v2.SessionRecord{Seat: &v2.Seat{PaneID: "pane-live", TerminalID: "terminal-live"}}
}

func ceremonyRequest() Request {
	return Request{Operation: OperationReissueCredential, GUID: "guid-live"}
}
