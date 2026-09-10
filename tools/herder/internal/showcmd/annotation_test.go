package showcmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/hcomidentity"
)

func TestShowPrintsAnnotationAttribution(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := agentstore.Open(state, nil)
	if _, err := store.Append(agentstore.Event{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindAnnotate, Name: "pini", Title: "display names", By: "web-alice-example-com", ByKind: "web"}); err != nil {
		t.Fatal(err)
	}
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{{Name: "pini"}}, nil }}
	var out, stderr bytes.Buffer
	if code := run([]string{"pini"}, &out, &stderr, deps); code != 0 {
		t.Fatalf("code=%d err=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "annotated        2026-09-10T12:00:00Z by web-alice-example-com") {
		t.Fatalf("out=%s", out.String())
	}
}
