package listcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/continuationstate"
	"ai-config/tools/herder/internal/registry"
)

func TestFailedContinuationIsVisibleAndExplicitlyAcknowledged(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", stateDir)
	rec := continuationstate.Record{
		ID: "compact-then-self-42", Status: "failed", Target: "worker-hone",
		UpdatedAt: "2026-07-12T12:00:00Z", Reason: "turn end never proven",
		LogPath: "/tmp/sender.log", RecoveryCommand: "herder send worker-hone -- 'continue'",
	}
	if err := continuationstate.Write("", rec); err != nil {
		t.Fatal(err)
	}
	var visible bytes.Buffer
	renderContinuationFailures(&visible, []continuationstate.Record{rec})
	for _, want := range []string{rec.ID, "@worker-hone", rec.Reason, rec.RecoveryCommand, rec.LogPath, "--ack-continuation"} {
		if !strings.Contains(visible.String(), want) {
			t.Fatalf("failure surface missing %q:\n%s", want, visible.String())
		}
	}

	binDir := t.TempDir()
	for _, name := range []string{"herdr", "jq"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	var stdout, stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("list exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "UNRESOLVED DETACHED CONTINUATIONS") || !strings.Contains(stdout.String(), rec.ID) {
		t.Fatalf("routine list surface omitted failure:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--ack-continuation", rec.ID}, &stdout, &stderr); code != 0 {
		t.Fatalf("ack exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "if recovery is still needed") {
		t.Fatalf("ack output dropped recovery remedy: %s", stdout.String())
	}
	failed, err := continuationstate.Unresolved("")
	if err != nil || len(failed) != 0 {
		t.Fatalf("unresolved after ack = %+v, %v", failed, err)
	}
}

func TestContextSnapshotDisplayNormalizesPercent(t *testing.T) {
	rec := writeContextSnapshot(t, "fresh-rive", "CTX_PCT=+42\nCTX_TS=100\n")

	got := contextSnapshotDisplay(rec, time.Unix(100, 0))
	if got != "42%" {
		t.Fatalf("contextSnapshotDisplay = %q, want normalized 42%%", got)
	}
}

func TestContextSnapshotDisplayRejectsHostilePercent(t *testing.T) {
	for _, pct := range []string{"Inf", "1e300", "0x1p10", strings.Repeat("9", 26)} {
		t.Run(pct, func(t *testing.T) {
			rec := writeContextSnapshot(t, "worker-rive", "CTX_PCT="+pct+"\nCTX_TS=100\n")

			got := contextSnapshotDisplay(rec, time.Unix(100, 0))
			if got != "unknown" {
				t.Fatalf("contextSnapshotDisplay(%q) = %q, want unknown", pct, got)
			}
		})
	}
}

func TestContextSnapshotDisplayRejectsFarFutureTimestamp(t *testing.T) {
	rec := writeContextSnapshot(t, "future-rive", "CTX_PCT=24\nCTX_TS=9223372036854775807\n")

	got := contextSnapshotDisplay(rec, time.Unix(100, 0))
	if got != "unknown" {
		t.Fatalf("future contextSnapshotDisplay = %q, want unknown", got)
	}
}

func writeContextSnapshot(t *testing.T, name, content string) registry.Record {
	t.Helper()
	root := t.TempDir()
	statusDir := filepath.Join(root, "statusline")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(statusDir, name+".env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return registry.Record{HcomDir: root, HcomName: name}
}
