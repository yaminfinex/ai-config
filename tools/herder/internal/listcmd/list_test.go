package listcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/registry"
)

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
