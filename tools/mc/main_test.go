package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshJournalFailsClosedOnUnparseableBusHead(t *testing.T) {
	dir := t.TempDir()
	hcom := writeExecutable(t, dir, "hcom", `#!/bin/sh
case "$*" in
  *"--last 1"*) printf '%s\n' 'notice: a new version is available'; exit 0 ;;
esac
printf '%s\n' '{"id":1,"type":"message","data":{"from":"agent-a","text":"old raise","mentions":["owner"]}}'
printf '%s\n' '{"id":2,"type":"message","data":{"from":"agent-a","text":"old followup","mentions":["owner"],"thread":"desk-1"}}'
`)
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	b := &Bus{Hcom: hcom}

	err = initializeCursor(s, b, false)
	if err == nil {
		// This is what startup would do next if the malformed head were mistaken
		// for an empty bus; retain it here so the regression proves the flood.
		_ = NewIngestor(s, b, "human-yamen", "owner").Tick()
	}
	if err == nil || !strings.Contains(err.Error(), "unparseable bus head") {
		t.Fatalf("startup error = %v, want unparseable bus head", err)
	}
	if got := s.List("", ""); len(got) != 0 {
		t.Fatalf("startup opened %d historical desk threads after malformed head", len(got))
	}
}
