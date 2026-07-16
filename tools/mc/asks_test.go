package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const askTestID = "ask-019b2d6e-7c18-7f65-9d8d-4db7efc3b4ec"

func testAskEntity() askEntity {
	return askEntity{
		ID: askTestID, Kind: "ask", State: "open", Asker: "human-yamen", AddressedTo: "builder-koze",
		CreatedAt: "2026-07-16T01:12:40Z", UpdatedAt: "2026-07-16T01:20:03Z", Expects: "decide",
		Blocking: &askBlocking{Fact: "TASK-43 waits for the ruling", Actor: "human-yamen", At: "2026-07-16T01:12:40Z"},
		Anchor:   askReference{Type: "task", Ref: "TASK-43"}, Links: []askReference{{Type: "thread", Ref: "task-43-mc-asks"}},
		Members: []string{"human-yamen", "builder-koze"},
		Framing: askFraming{
			Context: "Choose the exact contract.", Question: "Which contract?", SubDecisions: []string{"read path"},
			Options:        []askOption{{ID: "a", Label: "mish", Cost: "wait", Risk: "sequencing", BlastRadius: "mc"}, {ID: "b", Label: "files", Cost: "writer", Risk: "drift", BlastRadius: "missions"}},
			Recommendation: &askRecommendation{Choice: "a", Reason: "one boundary"}, DoNothing: "W2 stays blocked.",
		},
		Replies: []askReply{{ID: "reply-1", Actor: "builder-koze", At: "2026-07-16T01:19:00Z", Prose: "Use mish."}},
		Traces: []askTrace{
			{Action: "widen-membership", Actor: "human-yamen", At: "2026-07-16T01:18:00Z", Member: "reviewer-tiru"},
			{Action: "withdraw", Actor: "vile", At: "2026-07-16T01:18:30Z", Outcome: "superseded", Reason: "later ruling", Citation: &askReference{Type: "entity", Ref: "ask-later"}},
		},
	}
}

func askStatusPayload(t *testing.T, entity askEntity) string {
	t.Helper()
	status := missionStatus{
		OK: true, Slug: "mission-one", MissionDir: "/missions/mission-one",
		Manifest: missionManifest{Mission: "mission-one", Authority: "vile", Owner: "human-yamen", Status: "active"},
		Asks:     missionAsks{Available: true, Counts: []missionAskCount{{State: "open", Count: 1}, {State: "closed", Count: 0}}, Total: 1, Entities: []askEntity{entity}},
	}
	b, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func askEnvelopePayload(t *testing.T, entity askEntity) string {
	t.Helper()
	b, err := json.Marshal(askEnvelope{OK: true, Verb: "asks.view", Slug: "mission-one", Entity: entity})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func askWeb(t *testing.T, user, mish string) *Web {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return NewWeb(s, &Bus{}, nil, user, "owner", "", newMissionResolver(mish, ""))
}

func TestAsksPagesRenderStatusAndViewContractsWithJSOff(t *testing.T) {
	dir := t.TempDir()
	entity := testAskEntity()
	status := askStatusPayload(t, entity)
	all := "[" + status + "]"
	view := askEnvelopePayload(t, entity)
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
if [ "$1" = "status" ] && [ "$2" = "--all" ]; then printf '%%s\n' %q
elif [ "$1" = "status" ]; then printf '%%s\n' %q
elif [ "$1" = "asks" ] && [ "$4" = "view" ]; then printf '%%s\n' %q
else exit 2
fi
`, all, status, view))
	w := askWeb(t, "human-yamen", mish)

	for _, tc := range []struct {
		path  string
		wants []string
	}{
		{"/", []string{"Zone 1 — answers from them", "Which contract?", "builder-koze", "Use mish.", "board · mish status"}},
		{"/asks", []string{"Asks boards", "/mission/mission-one/asks", "open 1", "mish status --all"}},
		{"/mission/mission-one/asks", []string{"mission-one asks", "Which contract?", "TASK-43", "attested"}},
		{"/ask/" + askTestID, []string{"Which contract?", "human-yamen ⇄ builder-koze", "cost: wait", "Use mish.", "Co-custodian traces", "citation", "Widen membership", "Review settlement", "mish asks view"}},
	} {
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rw.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", tc.path, rw.Code, rw.Body.String())
		}
		for _, want := range tc.wants {
			if !strings.Contains(rw.Body.String(), want) {
				t.Errorf("GET %s missing %q", tc.path, want)
			}
		}
	}

	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/node/asks", nil))
	if rw.Code != http.StatusNotFound {
		t.Fatalf("/node/asks = %d, want separately blocked 404", rw.Code)
	}
}

func TestAskWidenRendersOnlyForOwnerActorString(t *testing.T) {
	dir := t.TempDir()
	entity := testAskEntity()
	status, view := askStatusPayload(t, entity), askEnvelopePayload(t, entity)
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = status ] && [ \"$2\" = --all ]; then printf '%%s\\n' %q; elif [ \"$1\" = status ]; then printf '%%s\\n' %q; else printf '%%s\\n' %q; fi\n", "["+status+"]", status, view))
	w := askWeb(t, "human-other", mish)
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/ask/"+askTestID, nil))
	if strings.Contains(rw.Body.String(), "Widen membership") || strings.Contains(rw.Body.String(), "/widen") {
		t.Fatal("non-owner actor received widen affordance")
	}
}

func TestAskReplyUsesExactMishTransitionAndStalePreservesDraft(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "call")
	entity := testAskEntity()
	status := askStatusPayload(t, entity)
	current := entity
	current.UpdatedAt = "2026-07-16T01:21:04Z"
	view := askEnvelopePayload(t, current)
	refusal := `{"ok":false,"verb":"asks.reply","refusal":"stale_write","reason":"entity changed"}`
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
if [ "$1" = "status" ] && [ "$2" = "--all" ]; then printf '%%s\n' %q; exit 0; fi
if [ "$1" = "status" ]; then printf '%%s\n' %q; exit 0; fi
if [ "$1" = "asks" ] && [ "$4" = "view" ]; then printf '%%s\n' %q; exit 0; fi
printf '%%s\n' "$*" > %q
cat >> %q
printf '%%s\n' %q
exit 1
`, "["+status+"]", status, view, logPath, logPath, refusal))
	w := askWeb(t, "human-yamen", mish)
	form := url.Values{"if_updated_at": {entity.UpdatedAt}, "prose": {"my unsent answer"}}
	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ask/"+askTestID+"/reply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w.Routes().ServeHTTP(rw, req)
	if rw.Code != http.StatusSeeOther {
		t.Fatalf("POST reply = %d: %s", rw.Code, rw.Body.String())
	}
	location := rw.Header().Get("Location")
	if !strings.Contains(location, "stale_write") || !strings.Contains(location, "my+unsent+answer") {
		t.Fatalf("stale redirect did not preserve refusal and draft: %s", location)
	}
	call, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(call)
	for _, want := range []string{"asks --mission mission-one reply " + askTestID + " --input -", `"actor":"human-yamen"`, `"if_updated_at":"2026-07-16T01:20:03Z"`, `"prose":"my unsent answer"`} {
		if !strings.Contains(got, want) {
			t.Errorf("mish call missing %q: %s", want, got)
		}
	}
	refresh := httptest.NewRecorder()
	w.Routes().ServeHTTP(refresh, httptest.NewRequest(http.MethodGet, location, nil))
	for _, want := range []string{"my unsent answer", current.UpdatedAt, "stale_write"} {
		if !strings.Contains(refresh.Body.String(), want) {
			t.Errorf("stale refresh missing %q: %s", want, refresh.Body.String())
		}
	}
}

func TestAskSettlementIsTwoStepAndWithdrawIsNeverExposed(t *testing.T) {
	dir := t.TempDir()
	entity := testAskEntity()
	status, view := askStatusPayload(t, entity), askEnvelopePayload(t, entity)
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = status ] && [ \"$2\" = --all ]; then printf '%%s\\n' %q; elif [ \"$1\" = status ]; then printf '%%s\\n' %q; else printf '%%s\\n' %q; fi\n", "["+status+"]", status, view))
	w := askWeb(t, "human-yamen", mish)
	for _, path := range []string{"/ask/" + askTestID, "/ask/" + askTestID + "?confirm=settle&choice=a&prose=ship+it"} {
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, path, nil))
		body := rw.Body.String()
		if strings.Contains(body, "/withdraw") || strings.Contains(body, "withdraw-with-citation") {
			t.Fatalf("%s exposed withdrawal: %s", path, body)
		}
		if strings.Contains(path, "confirm") && (!strings.Contains(body, "Confirm settlement") || !strings.Contains(body, "ship it")) {
			t.Fatalf("confirmation state missing: %s", body)
		}
	}
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodPost, "/ask/"+askTestID+"/withdraw", nil))
	if rw.Code != http.StatusNotFound {
		t.Fatalf("withdraw route = %d, want 404", rw.Code)
	}
	rw = httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodPost, "/ask/"+askTestID+"/settle", nil))
	if rw.Code != http.StatusSeeOther || !strings.Contains(rw.Header().Get("Location"), "requires+confirmation") {
		t.Fatalf("direct settlement bypass was not refused: %d %s", rw.Code, rw.Header().Get("Location"))
	}
}

func TestAskPostsMapOneToOneToNamedMishTransitions(t *testing.T) {
	entity := testAskEntity()
	status := askStatusPayload(t, entity)
	cases := []struct {
		name, route, verb string
		form              url.Values
		want              []string
	}{
		{"settle", "settle", "settle", url.Values{"confirmed": {"true"}, "if_updated_at": {entity.UpdatedAt}, "choice": {"a"}, "prose": {"ship"}}, []string{`"choice":"a"`, `"prose":"ship"`}},
		{"close-one-tap", "close", "close", url.Values{"if_updated_at": {entity.UpdatedAt}, "outcome": {"no-action"}}, []string{`"outcome":"no-action"`, `"reason":"No additional reason supplied."`}},
		{"link", "link", "link", url.Values{"if_updated_at": {entity.UpdatedAt}, "link_type": {"artifact"}, "link_ref": {"artifacts/design.md"}, "set_anchor": {"true"}}, []string{`"type":"artifact"`, `"ref":"artifacts/design.md"`, `"set_anchor":true`}},
		{"widen", "widen", "widen-membership", url.Values{"if_updated_at": {entity.UpdatedAt}, "member": {"reviewer-tiru"}}, []string{`"member":"reviewer-tiru"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "call")
			success := askEnvelopePayload(t, entity)
			mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
if [ "$1" = "status" ]; then printf '%%s\n' %q; exit 0; fi
printf '%%s\n' "$*" > %q
cat >> %q
printf '%%s\n' %q
`, "["+status+"]", logPath, logPath, success))
			w := askWeb(t, "human-yamen", mish)
			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/ask/"+askTestID+"/"+tc.route, strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w.Routes().ServeHTTP(rw, req)
			if rw.Code != http.StatusSeeOther {
				t.Fatalf("POST = %d: %s", rw.Code, rw.Body.String())
			}
			raw, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			got := string(raw)
			for _, want := range append([]string{"asks --mission mission-one " + tc.verb + " " + askTestID + " --input -", `"actor":"human-yamen"`, `"if_updated_at":"` + entity.UpdatedAt + `"`}, tc.want...) {
				if !strings.Contains(got, want) {
					t.Errorf("mish call missing %q: %s", want, got)
				}
			}
		})
	}
}

func TestAskGroupingUsesOnlyStoredAnchor(t *testing.T) {
	a := testAskEntity()
	b := testAskEntity()
	b.ID = "ask-other"
	b.Links = []askReference{{Type: "task", Ref: "TASK-DIFFERENT"}}
	c := testAskEntity()
	c.ID = "ask-third"
	c.Anchor = askReference{Type: "artifact", Ref: "artifacts/design.md"}
	c.Links = []askReference{{Type: "task", Ref: "TASK-43"}}
	groups := groupAsks([]askEntity{a, b, c}, nil)
	if len(groups) != 2 || len(groups[0].Entities) != 2 || groups[0].Anchor.Ref != "TASK-43" || groups[1].Anchor.Ref != "artifacts/design.md" {
		t.Fatalf("links changed anchor grouping: %#v", groups)
	}
}
