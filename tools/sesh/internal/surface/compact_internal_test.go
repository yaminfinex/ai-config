package surface

import (
	"strings"
	"testing"

	"sesh/internal/wire"
)

// conv renders Claude JSONL lines through the real parser and the compact
// builder, one display entry per line (role as the indexer would assign).
func conv(t *testing.T, lines ...string) []convRow {
	t.Helper()
	var entries []displayEntry
	for _, line := range lines {
		role := "unknown"
		switch {
		case strings.Contains(line, `"role":"user"`):
			role = "user"
		case strings.Contains(line, `"role":"assistant"`):
			role = "assistant"
		}
		blocks := renderLine("claude", []byte(line), int64(len(line)))
		entries = append(entries, displayEntry{Role: role, Blocks: blocks})
	}
	return compactRows(entries)
}

func pills(rows []convRow) []convPill {
	var out []convPill
	for _, r := range rows {
		out = append(out, r.Pills...)
	}
	return out
}

func TestCompactPairsToolResultWithItsCall(t *testing.T) {
	rows := conv(t,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls -la","description":"List files"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"total 0","is_error":true}]}}`,
	)
	ps := pills(rows)
	if len(rows) != 1 || len(ps) != 1 {
		t.Fatalf("want one run with one pill, got %d rows %+v", len(rows), ps)
	}
	p := ps[0]
	if p.Label != "Bash: List files" || !p.HasResult || p.Result != "total 0" || p.Tone != "error" {
		t.Fatalf("tool pill = %+v, want labelled call carrying its error result", p)
	}
}

func TestCompactHidesBookkeepingKeepsUnknownVisible(t *testing.T) {
	rows := conv(t,
		`{"type":"atis-latch","atis":"","sessionId":"s"}`,
		`{"type":"mode","mode":"normal","sessionId":"s"}`,
		`{"type":"attachment","attachment":{"type":"prompt_snapshot","systemPrompt":"x"}}`,
		`{"type":"system","subtype":"turn_duration","durationMs":5}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"sig"}]}}`,
		`{"type":"attachment","attachment":{"type":"brand_new_kind","x":1}}`,
		`{"type":"future-probe","sessionId":"s"}`,
	)
	ps := pills(rows)
	if len(ps) != 2 || ps[0].Label != "attachment · brand_new_kind" || ps[1].Label != "future-probe" {
		t.Fatalf("pills = %+v, want only the two unknown shapes visible", ps)
	}
}

func TestCompactFoldsHookNoticeAndContext(t *testing.T) {
	rows := conv(t,
		`{"type":"attachment","attachment":{"type":"hook_system_message","hookEvent":"PostToolUse","content":"[inform #1] a → b: done"}}`,
		`{"type":"attachment","attachment":{"type":"hook_additional_context","hookEvent":"PostToolUse","content":["<hcom>[inform #1] a → b: done, full text</hcom>"]}}`,
	)
	ps := pills(rows)
	if len(ps) != 1 || ps[0].Label != "[inform #1] a → b: done" || ps[0].Tone != "message" || !strings.Contains(ps[0].Detail, "full text") {
		t.Fatalf("pills = %+v, want one message pill labelled by the notice", ps)
	}
}

func TestCompactBusRequestReadsAsMessage(t *testing.T) {
	rows := conv(t,
		`{"type":"attachment","attachment":{"type":"hook_system_message","hookEvent":"UserPromptSubmit","content":"[request #9] op → me: please look"}}`,
		`{"type":"attachment","attachment":{"type":"hook_additional_context","hookEvent":"UserPromptSubmit","content":["<hcom>[request #9] op → me: [HERDER_WEB_OPERATOR_NOTE_BEGIN]\nboilerplate\n[HERDER_WEB_OPERATOR_NOTE_END]\n\nplease look at X</hcom>"]}}`,
	)
	if len(rows) != 1 || rows[0].Kind != "message" || rows[0].Speaker != "hcom" {
		t.Fatalf("rows = %+v, want the request alone as an hcom message", rows)
	}
	if strings.Contains(rows[0].Text, "boilerplate") || !strings.Contains(rows[0].Text, "please look at X") {
		t.Fatalf("message text = %q, want the operator note stripped", rows[0].Text)
	}
}

func TestCompactDividerAndFencing(t *testing.T) {
	rows := conv(t,
		`{"type":"system","subtype":"compact_boundary","content":"Conversation compacted"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"<status>Working on it.</status>\n<internal>scratch</internal>\nThe answer is 4."}]}}`,
	)
	if len(rows) != 3 || rows[0].Kind != "divider" || rows[1].Kind != "run" || rows[2].Kind != "message" {
		t.Fatalf("rows = %+v, want divider, fencing pills, prose", rows)
	}
	if ps := rows[1].Pills; len(ps) != 2 || ps[0].Label != "Working on it." || ps[1].Detail != "scratch" {
		t.Fatalf("fencing pills = %+v", ps)
	}
	if rows[2].Text != "The answer is 4." {
		t.Fatalf("prose = %q", rows[2].Text)
	}
}

func TestCompactUserHarnessTextIsNotProse(t *testing.T) {
	rows := conv(t,
		`{"type":"user","message":{"role":"user","content":"<command-name>/clear</command-name>\n<command-args></command-args>"}}`,
		`{"type":"user","message":{"role":"user","content":"<local-command-caveat>ignore</local-command-caveat>"}}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"Base directory for this skill: /x"}}`,
		`{"type":"user","message":{"role":"user","content":"fix the bug"}}`,
	)
	if len(rows) != 2 || rows[1].Kind != "message" || rows[1].Text != "fix the bug" {
		t.Fatalf("rows = %+v, want harness pills then the real prompt", rows)
	}
	if ps := rows[0].Pills; len(ps) != 2 || ps[0].Label != "/clear" || ps[1].Label != "injected context" {
		t.Fatalf("pills = %+v", ps)
	}
}

func TestProseEscapesAndFormats(t *testing.T) {
	got := string(prose("<b>x</b> **bold** `a**b**<i>`\n# Head"))
	want := "&lt;b&gt;x&lt;/b&gt; <strong>bold</strong> <code>a**b**&lt;i&gt;</code>\n<strong>Head</strong>"
	if got != want {
		t.Fatalf("prose = %q\nwant    %q", got, want)
	}
	if got := string(prose("**wrap `c` it**")); got != "<strong>wrap <code>c</code> it</strong>" {
		t.Fatalf("bold around a code span = %q", got)
	}
	if got := string(prose("odd ` tick **b**")); got != "odd ` tick <strong>b</strong>" {
		t.Fatalf("unpaired backtick prose = %q", got)
	}
}

func convTool(tool wire.Tool, lines ...string) []convRow {
	var entries []displayEntry
	for _, line := range lines {
		entries = append(entries, displayEntry{Role: "unknown", Blocks: renderLine(tool, []byte(line), int64(len(line)))})
	}
	return compactRows(entries)
}

func TestCompactCodexRollout(t *testing.T) {
	rows := convTool(wire.ToolCodex,
		`{"type":"session_meta","payload":{"id":"x","cwd":"/w"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"<permissions instructions>a"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"<skills_instructions>b"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions for /w"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"review PR 7"}]}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"review PR 7"}}`,
		`{"type":"response_item","payload":{"type":"reasoning","summary":[],"encrypted_content":"gAAA"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","call_id":"c1","name":"exec","input":"text(await tools.exec_command({cmd:\"gh pr diff 7\"}))"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"c1","output":[{"type":"input_text","text":"diff --git"}]}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{}}}`,
		`{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"<hcom>[request:t #5] a → b: look again</hcom>"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"APPROVE"}]}}`,
	)
	var got []string
	for _, r := range rows {
		if r.Kind == "message" {
			got = append(got, r.Speaker+": "+r.Text)
			continue
		}
		for _, p := range r.Pills {
			got = append(got, "pill: "+p.Label)
			if p.Label == "exec: gh pr diff 7" && (!p.HasResult || p.Result != "diff --git") {
				t.Errorf("exec pill = %+v, want its output paired", p)
			}
		}
	}
	want := []string{
		"pill: developer instructions", "pill: injected context", "user: review PR 7",
		"pill: exec: gh pr diff 7", "hcom: [request:t #5] a → b: look again", "assistant: APPROVE",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("codex rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestCompactGrokSeparatesQueryFromInjectedContext(t *testing.T) {
	rows := convTool(wire.ToolGrok,
		`{"type":"user","content":[{"type":"text","text":"<user_info>\nOS: linux"}]}`,
		`{"type":"user","content":[{"type":"text","text":"<system-reminder>skills</system-reminder>"}],"synthetic_reason":"system_reminder"}`,
		`{"type":"user","content":[{"type":"text","text":"<user_query>\nrun ls\n</user_query>"}]}`,
		`{"type":"assistant","content":"","tool_calls":[{"id":"k1","name":"run_terminal_command","arguments":"{\"command\":\"ls\",\"description\":\"List\"}"}]}`,
		`{"type":"tool_result","tool_call_id":"k1","content":"a.txt"}`,
	)
	if len(rows) != 3 || rows[1].Kind != "message" || rows[1].Text != "run ls" {
		t.Fatalf("rows = %+v, want context pills, the query as prose, the tool run", rows)
	}
	if ps := rows[0].Pills; len(ps) != 2 || ps[0].Label != "injected context" || ps[1].Label != "system reminder" {
		t.Fatalf("context pills = %+v", ps)
	}
	if ps := rows[2].Pills; len(ps) != 1 || ps[0].Label != "run_terminal_command: List" || ps[0].Result != "a.txt" {
		t.Fatalf("tool pills = %+v", ps)
	}
}

func TestProseFencedBlock(t *testing.T) {
	got := string(prose("Output:\n\n```text\n<ok>\n```\n\ndone"))
	want := "Output:\n\n<code class=\"fence\">&lt;ok&gt;</code>\ndone"
	if got != want {
		t.Fatalf("prose = %q\nwant    %q", got, want)
	}
}

// A session with no index rows is not necessarily quarantined: its rows may
// simply not be indexed yet, or it may hold no messages at all. The badge
// says quarantined only when quarantined rows exist.
func TestRecencyRawOnlyBadgeNamesTheCause(t *testing.T) {
	tmpl := mustPage("recency.html")
	for _, tc := range []struct {
		quarantined int
		want        string
	}{
		{quarantined: 2, want: "quarantined — raw only"},
		{quarantined: 0, want: "no messages indexed — raw only"},
	} {
		sum := SessionSummary{Tool: wire.ToolClaude, LogicalSessionID: "92c419c4-3bfe-4c8d-a2e6-380018362035", QuarantinedRows: tc.quarantined}
		var b strings.Builder
		page := recencyPage{Sessions: sessionItems([]SessionSummary{sum}), Total: 1, From: 1, To: 1}
		if err := tmpl.ExecuteTemplate(&b, "recencyBody", page); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), tc.want) {
			t.Errorf("quarantined rows %d: badge lacks %q", tc.quarantined, tc.want)
		}
		if tc.quarantined == 0 && strings.Contains(b.String(), "quarantined") {
			t.Errorf("a session with no quarantined rows is badged quarantined")
		}
	}
}
