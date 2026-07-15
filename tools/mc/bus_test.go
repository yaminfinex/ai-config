package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakePagedHcom(t *testing.T, head int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "queries.log")
	path := filepath.Join(dir, "hcom")
	script := fmt.Sprintf(`#!/bin/sh
set -eu
printf '%%s\n' "$*" >> %q
sql=
mention=
tail=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --sql) sql=$2; shift 2 ;;
    --mention) mention=$2; shift 2 ;;
    --last) tail=$2; shift 2 ;;
    *) shift ;;
  esac
done
case "$sql" in
  *"id IN"*) ;;
  *)
    start=$((%d - tail + 1))
    i=$start
    while [ "$i" -le %d ]; do
      printf '{"id":%%d,"type":"message","data":{"mentions":[]}}\n' "$i"
      i=$((i + 1))
    done
    exit 0
    ;;
esac
cursor=$(printf '%%s\n' "$sql" | sed -n 's/.*id > \([0-9][0-9]*\).*/\1/p')
limit=$(printf '%%s\n' "$sql" | sed -n 's/.*LIMIT \([0-9][0-9]*\).*/\1/p')
[ -n "$cursor" ] && [ -n "$limit" ] || exit 2
last=$((cursor + limit))
[ "$last" -le %d ] || last=%d
i=$last
while [ "$i" -gt "$cursor" ]; do
  if [ -n "$mention" ]; then
    printf '{"id":%%d,"type":"message","data":{"mentions":["%%s"]}}\n' "$i" "$mention"
  else
    printf '{"id":%%d,"type":"message","data":{"mentions":[]}}\n' "$i"
  fi
  i=$((i - 1))
done
`, logPath, head, head, head, head)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, logPath
}

func TestEventsSincePagesForwardWithoutSkipping(t *testing.T) {
	hcom, logPath := fakePagedHcom(t, 1205)
	b := &Bus{Hcom: hcom}

	evs, err := b.EventsSince(0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1205 || evs[0].ID != 1 || evs[len(evs)-1].ID != 1205 {
		t.Fatalf("events = %d ids %d..%d, want 1205 ids 1..1205", len(evs), evs[0].ID, evs[len(evs)-1].ID)
	}
	queries, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	q := string(queries)
	if strings.Count(q, "id IN (SELECT id") != 3 {
		t.Fatalf("each limited query must bound oldest-page membership:\n%s", q)
	}
	for _, cursor := range []string{"id > 0", "id > 500", "id > 1000"} {
		if !strings.Contains(q, cursor) {
			t.Errorf("queries missing forward page %q:\n%s", cursor, q)
		}
	}
}

func TestMentionsSincePagesForwardWithoutSkipping(t *testing.T) {
	hcom, logPath := fakePagedHcom(t, 1205)
	b := &Bus{Hcom: hcom}

	evs, err := b.MentionsSince(0, 500, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1205 || evs[0].ID != 1 || evs[len(evs)-1].ID != 1205 {
		t.Fatalf("mentions = %d ids %d..%d, want 1205 ids 1..1205", len(evs), evs[0].ID, evs[len(evs)-1].ID)
	}
	queries, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	q := string(queries)
	if strings.Count(q, "id IN (SELECT id") != 3 {
		t.Fatalf("each limited mention query must bound oldest-page membership:\n%s", q)
	}
	if !strings.Contains(q, "json_each(msg_mentions)") {
		t.Fatalf("mention predicate is outside forward-page subquery:\n%s", q)
	}
}

func TestTickDrainsBacklogAndLandsCursorOnHead(t *testing.T) {
	hcom, logPath := fakePagedHcom(t, 1205)
	s, err := OpenStore(filepath.Join(t.TempDir(), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	in := NewIngestor(s, &Bus{Hcom: hcom}, "human-yamen", "owner")

	if err := in.Tick(); err != nil {
		t.Fatal(err)
	}
	if got := s.Cursor(); got != 1205 {
		t.Fatalf("cursor = %d, want true head 1205", got)
	}
	queries, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(queries), "id <= 1205") {
		t.Fatalf("mention enrichment was not bounded to captured head:\n%s", queries)
	}
}
