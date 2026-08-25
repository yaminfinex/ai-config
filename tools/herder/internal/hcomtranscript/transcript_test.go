package hcomtranscript

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowShellsThroughBoundedHcomTranscriptRanges(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	stub := filepath.Join(dir, "hcom")
	script := `#!/usr/bin/env bash
if [ -n "${HCOM_INSTANCE_NAME:-}${HCOM_PROCESS_ID:-}${HERDR_PANE_ID:-}" ] || [ "${HCOM_DIR:-}" != /fixture/hcom ]; then
  printf 'identity leaked or bus directory lost\n' >&2
  exit 3
fi
printf '%s\n' "$*" >>"$TRANSCRIPT_CALLS"
case " $* " in
  *" transcript dore --last 2 --json "*)
    printf '%s\n' '[{"position":4,"user":"four"},{"position":5,"user":"five"}]' ;;
  *" transcript dore 2-3 --json "*)
    printf '%s\n' '[{"position":2,"user":"two"},{"position":3,"user":"three"}]' ;;
  *" transcript dore 4-5 --detailed --json "*)
    printf '%s\n' '[{"position":4,"tools":["read"]},{"position":5,"tools":["write"]}]' ;;
  *) printf 'unbounded or unexpected transcript call: %s\n' "$*" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("TRANSCRIPT_CALLS", log)
	t.Setenv("HCOM_DIR", "/fixture/hcom")
	t.Setenv("HCOM_INSTANCE_NAME", "seated-agent")
	t.Setenv("HCOM_PROCESS_ID", "process")
	t.Setenv("HERDR_PANE_ID", "pane")

	latest, err := Window(context.Background(), "dore", 0, 2, Exchanges)
	if err != nil || len(latest) != 2 || latest[0].Position != 4 || latest[1].Position != 5 {
		t.Fatalf("latest=%#v err=%v", latest, err)
	}
	older, err := Window(context.Background(), "dore", 4, 2, Exchanges)
	if err != nil || len(older) != 2 || older[0].Position != 2 || older[1].Position != 3 {
		t.Fatalf("older=%#v err=%v", older, err)
	}
	detailed, err := Range(context.Background(), "dore", 4, 5, Full)
	if err != nil || len(detailed) != 2 || !strings.Contains(string(mustJSON(t, detailed[0])), `"tools"`) {
		t.Fatalf("detailed=%#v err=%v", detailed, err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := string(calls)
	if strings.Contains(got, "--last 10000") || !strings.Contains(got, "--last 2") || !strings.Contains(got, "2-3") || !strings.Contains(got, "--detailed") {
		t.Fatalf("hcom calls were not bounded/detail-pinned:\n%s", got)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
