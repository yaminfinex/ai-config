package eventcmd

import (
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
)

func TestParseSharedEventFlags(t *testing.T) {
	at := "2026-09-10T12:34:56.123Z"
	id := "12345678-1234-4123-8123-123456789abc"
	e, asJSON, err := Parse(agentstore.KindAssign, []string{"--name", " impl-geni ", "--manager", " ziru ", "--by", " owner ", "--by-kind", " user ", "--at", at, "--id", id, "--json"})
	if err != nil {
		t.Fatal(err)
	}
	wantAt, _ := time.Parse(time.RFC3339Nano, at)
	if !asJSON || e.Name != "impl-geni" || e.Manager != "ziru" || e.By != "owner" || e.ByKind != "user" || e.ID != id || !e.At.Equal(wantAt) {
		t.Fatalf("event=%+v json=%v", e, asJSON)
	}
}

func TestDefaultBy(t *testing.T) {
	t.Setenv("HCOM_NAME", "impl-geni")
	t.Setenv("HCOM_INSTANCE_NAME", "ignored")
	if by, kind := DefaultBy(); by != "impl-geni" || kind != "agent" {
		t.Fatalf("got %q/%q", by, kind)
	}
}
