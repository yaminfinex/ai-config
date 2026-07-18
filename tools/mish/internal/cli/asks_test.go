package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mish/internal/missionfs"
)

const cliAskID = "ask-019b2d6e-7c18-7f65-9d8d-4db7efc3b4ec"

func makeAsksDeps(t *testing.T) (deps, string) {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, "missions", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := missionfs.WriteManifest(filepath.Join(dir, "mission.md"), missionfs.Manifest{Mission: "demo", Authority: "vile", Owner: "owner", Status: "active", Created: "2026-07-16"}, "Demo"); err != nil {
		t.Fatal(err)
	}
	d := newTestDeps(repo, dir)
	d.clock = func() time.Time { return time.Date(2026, 7, 16, 1, 0, 0, 0, time.UTC) }
	return d, dir
}

func decideCreate(actor, addressed string) string {
	return `{"id":"` + cliAskID + `","kind":"ask","actor":"` + actor + `","addressed_to":"` + addressed + `","expects":"decide","anchor":{"type":"task","ref":"TASK-41"},"links":[{"type":"thread","ref":"task-38-asks-board"}],"framing":{"context":"ctx","question":"choose?","sub_decisions":[],"options":[{"id":"a","label":"A","cost":"c","risk":"r","blast_radius":"b"},{"id":"b","label":"B","cost":"c","risk":"r","blast_radius":"b"}],"recommendation":{"choice":"a","reason":"best"},"do_nothing":"blocked"}}`
}

func runAsksJSON(t *testing.T, d deps, input string, args ...string) (int, asksOutput, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	d.stdin = strings.NewReader(input)
	d.stdout = &stdout
	d.stderr = &stderr
	code := runWithDeps(args, d)
	var out asksOutput
	if stdout.Len() > 0 {
		_ = json.Unmarshal(stdout.Bytes(), &out)
	}
	return code, out, stdout.String(), stderr.String()
}

func TestAsksCreateInboundAndOutboundUseSameContract(t *testing.T) {
	for _, tc := range []struct{ name, actor, addressed string }{{"inbound", "vile", "owner"}, {"outbound", "owner", "vile"}} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := makeAsksDeps(t)
			code, out, raw, stderr := runAsksJSON(t, d, decideCreate(tc.actor, tc.addressed), "asks", "--mission", "demo", "create", "--input", "-")
			if code != exitOK || stderr != "" || out.Entity == nil {
				t.Fatalf("code=%d raw=%s stderr=%s", code, raw, stderr)
			}
			if out.Entity.Members[0] != tc.actor || out.Entity.Members[1] != tc.addressed || out.Entity.State != "open" {
				t.Fatalf("entity = %+v", out.Entity)
			}
			reply := `{"actor":"` + tc.addressed + `","if_updated_at":"` + out.Entity.UpdatedAt + `","prose":"reply"}`
			code, out, _, _ = runAsksJSON(t, d, reply, "asks", "--mission", "demo", "reply", cliAskID, "--input", "-")
			if code != 0 || len(out.Entity.Replies) != 1 {
				t.Fatalf("directional reply failed: %+v", out.Entity)
			}
			if tc.name == "outbound" {
				closeInput := `{"actor":"vile","if_updated_at":"` + out.Entity.UpdatedAt + `","outcome":"no-action","reason":"skip"}`
				closeCode, _, closeJSON, _ := runAsksJSON(t, d, closeInput, "asks", "--mission", "demo", "close", cliAskID, "--input", "-")
				if closeCode != 1 || !strings.Contains(closeJSON, "actor_not_authorized") {
					t.Fatalf("authority/addressee close bypass: code=%d %s", closeCode, closeJSON)
				}
			}
			settle := `{"actor":"` + tc.addressed + `","if_updated_at":"` + out.Entity.UpdatedAt + `","choice":"a","prose":"settled"}`
			code, out, _, _ = runAsksJSON(t, d, settle, "asks", "--mission", "demo", "settle", cliAskID, "--input", "-")
			if code != 0 || out.Entity.State != "closed" {
				t.Fatalf("directional settle failed: %+v", out.Entity)
			}
		})
	}
}

func TestAsksRejectsIncompleteDecisionWithoutWriting(t *testing.T) {
	d, dir := makeAsksDeps(t)
	input := strings.Replace(decideCreate("vile", "owner"), `"options":[{"id":"a","label":"A","cost":"c","risk":"r","blast_radius":"b"},{"id":"b","label":"B","cost":"c","risk":"r","blast_radius":"b"}]`, `"options":[]`, 1)
	code, _, stdout, _ := runAsksJSON(t, d, input, "asks", "--mission", "demo", "create", "--input", "-")
	if code != exitRefuse || !strings.Contains(stdout, "invalid_entity") {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "asks", "entities", cliAskID+".md")); !os.IsNotExist(err) {
		t.Fatalf("invalid entity was written: %v", err)
	}
}

func TestAsksReplySettleAndImmutableFraming(t *testing.T) {
	d, dir := makeAsksDeps(t)
	code, out, _, _ := runAsksJSON(t, d, decideCreate("vile", "owner"), "asks", "--mission", "demo", "create", "--input", "-")
	if code != 0 {
		t.Fatal("create")
	}
	stamp := out.Entity.UpdatedAt
	d.clock = func() time.Time { return time.Date(2026, 7, 16, 1, 0, 1, 0, time.UTC) }
	reply := `{"actor":"owner","if_updated_at":"` + stamp + `","prose":"considering"}`
	code, out, _, _ = runAsksJSON(t, d, reply, "asks", "--mission", "demo", "reply", cliAskID, "--input", "-")
	if code != 0 || len(out.Entity.Replies) != 1 {
		t.Fatalf("reply code=%d entity=%+v", code, out.Entity)
	}
	settle := `{"actor":"owner","if_updated_at":"` + out.Entity.UpdatedAt + `","choice":"a","prose":"choose A"}`
	d.clock = func() time.Time { return time.Date(2026, 7, 16, 1, 0, 2, 0, time.UTC) }
	code, out, _, stderr := runAsksJSON(t, d, settle, "asks", "--mission", "demo", "settle", cliAskID, "--input", "-")
	if code != 0 || stderr != "" || out.Entity.Kind != "ruling" || out.Entity.State != "closed" || *out.Entity.Outcome != "settled" || len(out.Entity.RulingTrail) != 1 {
		t.Fatalf("settle code=%d entity=%+v stderr=%s", code, out.Entity, stderr)
	}
	if out.Entity.Framing.Question != "choose?" || out.Entity.RulingTrail[0].Question != "choose?" || len(out.Entity.RulingTrail[0].OptionsAsPresented) != 2 {
		t.Fatalf("framing/snapshot = %+v", out.Entity)
	}
	data := string(mustReadCLI(t, filepath.Join(dir, "asks", "entities", cliAskID+".md")))
	if !strings.Contains(data, "question: choose?") {
		t.Fatalf("framing not preserved:\n%s", data)
	}
}

func TestAsksReplyRequiresMembershipAndMayAppendAfterClose(t *testing.T) {
	d, _ := makeAsksDeps(t)
	_, out, _, _ := runAsksJSON(t, d, decideCreate("vile", "owner"), "asks", "--mission", "demo", "create", "--input", "-")
	denied := `{"actor":"other","if_updated_at":"` + out.Entity.UpdatedAt + `","prose":"no"}`
	code, _, stdout, _ := runAsksJSON(t, d, denied, "asks", "--mission", "demo", "reply", cliAskID, "--input", "-")
	if code != 1 || !strings.Contains(stdout, "actor_not_member") {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	settle := `{"actor":"owner","if_updated_at":"` + out.Entity.UpdatedAt + `","choice":"a","prose":"done"}`
	_, out, _, _ = runAsksJSON(t, d, settle, "asks", "--mission", "demo", "settle", cliAskID, "--input", "-")
	reply := `{"actor":"vile","if_updated_at":"` + out.Entity.UpdatedAt + `","prose":"after close"}`
	code, out, _, _ = runAsksJSON(t, d, reply, "asks", "--mission", "demo", "reply", cliAskID, "--input", "-")
	if code != 0 || out.Entity.State != "closed" || len(out.Entity.Replies) != 1 {
		t.Fatalf("closed reply=%+v", out.Entity)
	}
}

func TestReplyIDGenerationFailureLeavesEntityByteExact(t *testing.T) {
	d, dir := makeAsksDeps(t)
	_, out, _, _ := runAsksJSON(t, d, decideCreate("vile", "owner"), "asks", "--mission", "demo", "create", "--input", "-")
	path := filepath.Join(dir, "asks", "entities", cliAskID+".md")
	before := string(mustReadCLI(t, path))
	d.askID = func(time.Time) (string, error) { return "", errors.New("entropy unavailable") }
	input := `{"actor":"owner","if_updated_at":"` + out.Entity.UpdatedAt + `","prose":"reply"}`
	code, _, stdout, _ := runAsksJSON(t, d, input, "asks", "--mission", "demo", "reply", cliAskID, "--input", "-")
	if code != 1 || !strings.Contains(stdout, "id_generation_failed") {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
	if after := string(mustReadCLI(t, path)); after != before {
		t.Fatal("ID generation failure changed entity")
	}
}

func TestAsksCloseWithdrawalLinkAndWidenGuards(t *testing.T) {
	t.Run("close accepts only non-settlement outcomes", func(t *testing.T) {
		d, _ := makeAsksDeps(t)
		create := strings.Replace(decideCreate("vile", "owner"), `"expects":"decide"`, `"expects":"act"`, 1)
		_, out, _, _ := runAsksJSON(t, d, create, "asks", "--mission", "demo", "create", "--input", "-")
		bad := `{"actor":"owner","if_updated_at":"` + out.Entity.UpdatedAt + `","outcome":"settled","reason":"answered"}`
		code, _, stdout, _ := runAsksJSON(t, d, bad, "asks", "--mission", "demo", "close", cliAskID, "--input", "-")
		if code != 1 || !strings.Contains(stdout, "invalid_input") {
			t.Fatalf("settled close code=%d stdout=%s", code, stdout)
		}
		good := `{"actor":"vile","if_updated_at":"` + out.Entity.UpdatedAt + `","outcome":"no-action","reason":"not needed"}`
		code, out, _, _ = runAsksJSON(t, d, good, "asks", "--mission", "demo", "close", cliAskID, "--input", "-")
		if code != 0 || out.Entity.State != "closed" || *out.Entity.Outcome != "no-action" || len(out.Entity.Traces) != 1 {
			t.Fatalf("close=%+v", out.Entity)
		}
	})
	t.Run("authority cannot clear decide with close", func(t *testing.T) {
		d, _ := makeAsksDeps(t)
		_, out, _, _ := runAsksJSON(t, d, decideCreate("vile", "owner"), "asks", "--mission", "demo", "create", "--input", "-")
		input := `{"actor":"vile","if_updated_at":"` + out.Entity.UpdatedAt + `","outcome":"no-action","reason":"skip"}`
		code, _, stdout, _ := runAsksJSON(t, d, input, "asks", "--mission", "demo", "close", cliAskID, "--input", "-")
		if code != 1 || !strings.Contains(stdout, "actor_not_authorized") {
			t.Fatalf("code=%d stdout=%s", code, stdout)
		}
	})
	t.Run("withdraw needs authority citation and no addressee reply", func(t *testing.T) {
		d, _ := makeAsksDeps(t)
		_, out, _, _ := runAsksJSON(t, d, decideCreate("vile", "owner"), "asks", "--mission", "demo", "create", "--input", "-")
		input := `{"actor":"vile","if_updated_at":"` + out.Entity.UpdatedAt + `","outcome":"superseded","reason":"later ruling","citation":{"type":"entity","ref":"ask-019b2d6e-7c18-7f65-9d8d-4db7efc3b4ed"}}`
		code, out, _, _ := runAsksJSON(t, d, input, "asks", "--mission", "demo", "withdraw-with-citation", cliAskID, "--input", "-")
		if code != 0 || out.Entity.State != "closed" || len(out.Entity.Traces) != 1 || out.Entity.Traces[0].Action != "withdraw" {
			t.Fatalf("code=%d entity=%+v", code, out.Entity)
		}
	})
	t.Run("withdrawal stop condition is exact authority and unanswered", func(t *testing.T) {
		d, _ := makeAsksDeps(t)
		_, out, _, _ := runAsksJSON(t, d, decideCreate("vile", "owner"), "asks", "--mission", "demo", "create", "--input", "-")
		missing := `{"actor":"vile","if_updated_at":"` + out.Entity.UpdatedAt + `","outcome":"superseded","reason":"later"}`
		code, _, stdout, _ := runAsksJSON(t, d, missing, "asks", "--mission", "demo", "withdraw-with-citation", cliAskID, "--input", "-")
		if code != 1 || !strings.Contains(stdout, "invalid_input") {
			t.Fatalf("citation gate code=%d stdout=%s", code, stdout)
		}
		bad := `{"actor":"other","if_updated_at":"` + out.Entity.UpdatedAt + `","outcome":"superseded","reason":"later","citation":{"type":"task","ref":"TASK-42"}}`
		code, _, stdout, _ = runAsksJSON(t, d, bad, "asks", "--mission", "demo", "withdraw-with-citation", cliAskID, "--input", "-")
		if code != 1 || !strings.Contains(stdout, "actor_not_authority") {
			t.Fatalf("authority stop gate code=%d stdout=%s", code, stdout)
		}
		reply := `{"actor":"owner","if_updated_at":"` + out.Entity.UpdatedAt + `","prose":"answered"}`
		_, out, _, _ = runAsksJSON(t, d, reply, "asks", "--mission", "demo", "reply", cliAskID, "--input", "-")
		answered := `{"actor":"vile","if_updated_at":"` + out.Entity.UpdatedAt + `","outcome":"superseded","reason":"later","citation":{"type":"task","ref":"TASK-42"}}`
		code, _, stdout, _ = runAsksJSON(t, d, answered, "asks", "--mission", "demo", "withdraw-with-citation", cliAskID, "--input", "-")
		if code != 1 || !strings.Contains(stdout, "answered_ask") {
			t.Fatalf("answered stop gate code=%d stdout=%s", code, stdout)
		}
	})
	t.Run("widen idempotent and link anchor explicit", func(t *testing.T) {
		d, _ := makeAsksDeps(t)
		_, out, _, _ := runAsksJSON(t, d, decideCreate("vile", "owner"), "asks", "--mission", "demo", "create", "--input", "-")
		denied := `{"actor":"vile","if_updated_at":"` + out.Entity.UpdatedAt + `","member":"reviewer"}`
		code, _, stdout, _ := runAsksJSON(t, d, denied, "asks", "--mission", "demo", "widen-membership", cliAskID, "--input", "-")
		if code != 1 || !strings.Contains(stdout, "actor_not_owner") {
			t.Fatalf("widen guard code=%d stdout=%s", code, stdout)
		}
		w := `{"actor":"owner","if_updated_at":"` + out.Entity.UpdatedAt + `","member":"reviewer"}`
		_, out, _, _ = runAsksJSON(t, d, w, "asks", "--mission", "demo", "widen-membership", cliAskID, "--input", "-")
		w = `{"actor":"owner","if_updated_at":"` + out.Entity.UpdatedAt + `","member":"reviewer"}`
		_, out, _, _ = runAsksJSON(t, d, w, "asks", "--mission", "demo", "widen-membership", cliAskID, "--input", "-")
		if len(out.Entity.Members) != 3 || len(out.Entity.Traces) != 1 {
			t.Fatalf("idempotent widen = %+v", out.Entity)
		}
		link := `{"actor":"reviewer","if_updated_at":"` + out.Entity.UpdatedAt + `","link":{"type":"artifact","ref":"artifacts/design.md"},"set_anchor":true}`
		code, out, _, _ = runAsksJSON(t, d, link, "asks", "--mission", "demo", "link", cliAskID, "--input", "-")
		if code != 0 || out.Entity.Anchor.Type != "artifact" || len(out.Entity.Links) != 2 {
			t.Fatalf("link = %+v", out.Entity)
		}
		link = `{"actor":"reviewer","if_updated_at":"` + out.Entity.UpdatedAt + `","link":{"type":"artifact","ref":"artifacts/design.md"},"set_anchor":false}`
		code, out, _, _ = runAsksJSON(t, d, link, "asks", "--mission", "demo", "link", cliAskID, "--input", "-")
		if code != 0 || len(out.Entity.Links) != 2 || out.Entity.Anchor.Type != "artifact" {
			t.Fatalf("idempotent link = %+v", out.Entity)
		}
		entityAnchor := `{"actor":"reviewer","if_updated_at":"` + out.Entity.UpdatedAt + `","link":{"type":"entity","ref":"ask-019b2d6e-7c18-7f65-9d8d-4db7efc3b4ed"},"set_anchor":true}`
		code, _, stdout, _ = runAsksJSON(t, d, entityAnchor, "asks", "--mission", "demo", "link", cliAskID, "--input", "-")
		if code != 1 || !strings.Contains(stdout, "invalid_anchor") {
			t.Fatalf("entity anchor code=%d stdout=%s", code, stdout)
		}
	})
}

func TestAsksUsageAndRefusalOutputModes(t *testing.T) {
	d, _ := makeAsksDeps(t)
	code, _, stdout, stderr := runAsksJSON(t, d, "", "asks", "wat")
	if code != exitUsage || stdout != "" || !strings.Contains(stderr, "unknown subcommand") {
		t.Fatalf("usage code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	code, _, stdout, stderr = runAsksJSON(t, d, "", "asks", "--mission", "demo", "view", "bad")
	if code != exitRefuse || !strings.Contains(stdout, `"ok":false`) || !strings.Contains(stderr, "mish asks.view:") {
		t.Fatalf("refusal code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	code, _, stdout, stderr = runAsksJSON(t, d, "", "asks", "--mission", "demo", "view", "bad", "--text")
	if code != exitRefuse || stdout != "" || !strings.Contains(stderr, "mish asks.view:") {
		t.Fatalf("text refusal code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	code, _, stdout, stderr = runAsksJSON(t, d, decideCreate("vile", "owner"), "asks", "--mission", "demo", "create", "--input", "-", "--text")
	if code != 0 || stderr != "" || !strings.Contains(stdout, cliAskID+" ask open decide") {
		t.Fatalf("text success code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestAsksEmptyListStillEmitsEntitiesAndCounts(t *testing.T) {
	d, _ := makeAsksDeps(t)
	code, _, stdout, stderr := runAsksJSON(t, d, "", "asks", "--mission", "demo", "list")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"entities":[]`) || !strings.Contains(stdout, `"counts":{"closed":0,"open":0}`) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestAsksEntityReferenceMustBeAskID(t *testing.T) {
	d, _ := makeAsksDeps(t)
	bad := strings.Replace(decideCreate("vile", "owner"), `"links":[{"type":"thread","ref":"task-38-asks-board"}]`, `"links":[{"type":"entity","ref":"anything"}]`, 1)
	code, _, stdout, _ := runAsksJSON(t, d, bad, "asks", "--mission", "demo", "create", "--input", "-")
	if code != 1 || !strings.Contains(stdout, "invalid_entity") {
		t.Fatalf("code=%d stdout=%s", code, stdout)
	}
}

func mustReadCLI(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
