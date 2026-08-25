package hcommessage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSendRequestPinsAttributedRequestIntent(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	stub := filepath.Join(dir, "hcom")
	script := `#!/usr/bin/env bash
if [ -n "${HCOM_INSTANCE_NAME:-}${HCOM_PROCESS_ID:-}${HERDR_PANE_ID:-}" ] || [ "${HCOM_DIR:-}" != /fixture/hcom ]; then
  printf 'identity leaked or bus directory lost\n' >&2
  exit 3
fi
for arg in "$@"; do printf '<%s>\n' "$arg"; done >"$SEND_ARGS"
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("SEND_ARGS", log)
	t.Setenv("HCOM_DIR", "/fixture/hcom")
	t.Setenv("HCOM_INSTANCE_NAME", "seated-agent")
	t.Setenv("HCOM_PROCESS_ID", "process")
	t.Setenv("HERDR_PANE_ID", "pane")
	if err := SendRequest(context.Background(), "dore", "web-alice-example-com", "please inspect tools"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "<send>\n<@dore>\n<--intent>\n<request>\n<--from>\n<web-alice-example-com>\n<-->\n<please inspect tools>\n"
	if string(raw) != want || strings.Contains(string(raw), "inform") {
		t.Fatalf("hcom send args:\n%s\nwant:\n%s", raw, want)
	}
}
