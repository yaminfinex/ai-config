package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHerderListParsesProducerMissionRows(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("testdata", "herder-list-missions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	bin := writeExecutable(t, t.TempDir(), "herder", fmt.Sprintf("#!/bin/sh\nexec /bin/cat %q\n", fixture))

	rows, err := HerderList(bin)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	assertMission := func(index int, slug, source string) {
		t.Helper()
		if rows[index].Mission == nil {
			t.Fatalf("row %d mission = nil", index)
		}
		if got := *rows[index].Mission; got.Slug != slug || got.Source != source {
			t.Fatalf("row %d mission = %#v, want slug %q source %q", index, got, slug, source)
		}
	}
	assertMission(0, "2026-07-15-mission-control", "marker")
	assertMission(1, "2026-07-15-mission-control", "cwd")
	if rows[2].Mission != nil {
		t.Fatalf("mission:null parsed as %#v", rows[2].Mission)
	}
	assertMission(3, "2026-07-15-mission-control", "explicit")
}

func TestGraphRefreshesHerderMembershipInsideCommunicationCacheWindow(t *testing.T) {
	dir := t.TempDir()
	mission := filepath.Join(dir, "mission")
	if err := os.WriteFile(mission, []byte("mission-one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	herder := writeExecutable(t, dir, "herder", fmt.Sprintf(`#!/bin/sh
slug=$(/bin/cat %q)
printf '%%s\n' "{\"kind\":\"session\",\"guid\":\"guid-agent\",\"label\":\"builder-agent\",\"tool\":\"codex\",\"role\":\"builder\",\"state\":\"seated\",\"provenance\":{\"cwd\":\"/work\",\"tool_session_id\":\"sid-agent\"},\"mission\":{\"slug\":\"$slug\",\"source\":\"explicit\"}}"
`, mission))
	hcom := writeExecutable(t, dir, "hcom", `#!/bin/sh
if [ "$1" = list ]; then
  printf '%s\n' '[{"name":"builder-agent","status":"active","session_id":"sid-agent"}]'
fi
`)
	_, store := testIngestor(t)
	w := NewWeb(store, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, nil)
	now := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)

	findMission := func(model *graphModel) string {
		t.Helper()
		for _, node := range model.Nodes {
			if node.Name == "builder-agent" {
				return node.Mission
			}
		}
		t.Fatal("builder-agent node missing")
		return ""
	}
	if got := findMission(w.graphData("30m", now)); got != "mission-one" {
		t.Fatalf("first mission = %q", got)
	}
	if err := os.WriteFile(mission, []byte("mission-two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := findMission(w.graphData("30m", now.Add(time.Second))); got != "mission-two" {
		t.Fatalf("cached-render mission = %q, want fresh herder membership", got)
	}
}

func TestRosterGroupsUseHerderMissionBeforeCwdFallback(t *testing.T) {
	dir := t.TempDir()
	fixture, err := filepath.Abs(filepath.Join("testdata", "herder-list-missions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(dir, "herder-calls")
	herder := writeExecutable(t, dir, "herder", fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexec /bin/cat %q\n", calls, fixture))
	hcom := writeExecutable(t, dir, "hcom", `#!/bin/sh
printf '%s\n' '[{"name":"builder-marker","status":"active","session_id":"019f64d0-d89e-72a3-bc2b-a0c6a0b0b509"},{"name":"designer-cwd","status":"active","session_id":"019f6475-e98a-7910-8173-1e46cefb3ebd"},{"name":"manual-null","status":"active","session_id":"6ce3081b-d06b-4e2b-a4b0-411483eac867"},{"name":"builder-forward","status":"active","session_id":"sid-forward"}]'
`)
	mish := writeExecutable(t, dir, "mish", "#!/bin/sh\nprintf '%s\\n' '{\"ok\":true,\"slug\":\"fallback-mission\"}'\n")
	_, store := testIngestor(t)
	w := NewWeb(store, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, newMissionResolver(mish, ""))

	groups, warning := w.rosterGroups(false)
	if warning != "" {
		t.Fatalf("roster warning = %q", warning)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %#v, want declared and fallback missions", groups)
	}
	byMission := map[string]rosterGroup{}
	for _, group := range groups {
		byMission[group.Mission] = group
	}
	declared := byMission["2026-07-15-mission-control"]
	if len(declared.Agents) != 3 {
		t.Fatalf("declared agents = %#v", declared.Agents)
	}
	sources := map[string]string{}
	for _, agent := range declared.Agents {
		sources[agent.Name] = agent.MissionSource
	}
	for name, want := range map[string]string{"builder-marker": "marker", "designer-cwd": "cwd", "builder-forward": "explicit"} {
		if got := sources[name]; got != want {
			t.Errorf("%s source = %q, want %q", name, got, want)
		}
	}
	fallback := byMission["fallback-mission"]
	if len(fallback.Agents) != 1 || fallback.Agents[0].Name != "manual-null" || fallback.Agents[0].MissionSource != "" {
		t.Fatalf("fallback agents = %#v", fallback.Agents)
	}

	rawCalls, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(rawCalls)); got != "list --json" {
		t.Fatalf("herder calls = %q, want only list --json", got)
	}
}
