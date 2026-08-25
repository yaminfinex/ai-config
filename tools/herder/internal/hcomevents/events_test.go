package hcomevents

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSubscribeShellsThroughHcomEventWait(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "hcom")
	script := `#!/usr/bin/env bash
if [ -n "${HCOM_INSTANCE_NAME:-}${HCOM_PROCESS_ID:-}${HERDR_PANE_ID:-}" ] || [ "${HCOM_DIR:-}" != /fixture/hcom ]; then
  printf 'identity leaked or bus directory lost\n' >&2
  exit 3
fi
case " $* " in
  *" events --wait 30 --full --type message --sql id > 41 "*|*" events --last 10000 --full --type message --sql id > 41 "*)
    printf '%s\n' '{"id":42,"ts":"2099-01-01T00:00:00.000001+00:00","type":"message","data":{"from":"vile","delivered_to":["dore"],"thread":"web-serve","text":"ship it"}}'
    if [[ " $* " == *" --last 10000 "* ]]; then
      printf '%s\n' '{"id":42,"ts":"2099-01-01T00:00:00.000001+00:00","type":"message","data":{"from":"vile","delivered_to":["dore"],"thread":"web-serve","text":"ship it"}}'
    fi
    ;;
  *" events --last 1 --full --type message "*)
    printf '%s\n' '{"id":41,"ts":"2098-01-01T00:00:00.000001+00:00","type":"message","data":{"text":"baseline"}}'
    ;;
  *) printf 'unexpected args: %s\n' "$*" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("HCOM_DIR", "/fixture/hcom")
	t.Setenv("HCOM_INSTANCE_NAME", "dore")
	t.Setenv("HCOM_PROCESS_ID", "fixture-process")
	t.Setenv("HERDR_PANE_ID", "fixture-pane")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got Message
	emitted := 0
	healthy := false
	err := Subscribe(ctx, &Cursor{}, func(message Message) error {
		emitted++
		got = message
		cancel()
		return nil
	}, func() error {
		healthy = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.From != "vile" || len(got.To) != 1 || got.To[0] != "dore" || got.Thread != "web-serve" || got.Text != "ship it" {
		t.Fatalf("message = %#v", got)
	}
	if !healthy {
		t.Fatal("subscription did not report a successful hcom wait")
	}
	if emitted != 1 {
		t.Fatalf("duplicate event emitted %d times", emitted)
	}
}

func TestDecodeRejectsNonJSON(t *testing.T) {
	if _, err := decode([]byte("subscription exploded")); err == nil {
		t.Fatal("decode accepted non-JSON output")
	}
}

func TestRunAcceptsHcomTimedOutEnvelopeWithNonzeroExit(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "hcom")
	if err := os.WriteFile(stub, []byte("#!/usr/bin/env bash\nprintf '%s\\n' '{\"timed_out\":true}'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	event, err := run(context.Background(), "events", "--wait", "30")
	if err != nil || !event.TimedOut {
		t.Fatalf("event=%#v err=%v", event, err)
	}
}

func TestQueryDecodesBurstInChronologicalOrder(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "hcom")
	script := `#!/usr/bin/env bash
printf '%s\n' \
  '{"id":41,"ts":"2099-01-01T00:00:00.000001+00:00","type":"message","data":{"text":"first"}}' \
  '{"id":42,"ts":"2099-01-01T00:00:00.000002+00:00","type":"message","data":{"text":"second"}}'
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	events, err := query(context.Background(), 10000, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Data.Text != "first" || events[1].Data.Text != "second" {
		t.Fatalf("events = %#v", events)
	}
}
