# Grok delivery mechanism matrix

Date: 2026-07-09
Author: task129-laze
Subject: xAI grok CLI 0.2.93 (`grok 0.2.93 (f00f96316d)`, installed at `~/.local/bin/grok`)
Unit type: investigate only; no herder/hcom production code changed.

This note follows up `docs/design/2026-07-09-grok-cli-characterization.md`, which already proved that passive hook stdout is discarded and that pty paste works but is unacceptable as the desired delivery mechanism. The question here is narrower: does grok expose any other mid-session delivery surface that can replace pty paste?

All empirical runs used scratch directories under `/tmp/task129-grok-*`, scratch `HOME`, scratch `GROK_HOME`, and scratch `HCOM_DIR`. No live registry or live hcom bus state was used. The only pre-existing credential used was `XAI_API_KEY`; no signup or account flow was attempted.

## Summary

The best non-pty delivery mechanism in grok 0.2.93 is **MCP tool polling**.

A bus-bridge MCP server can expose a `receive_messages` tool. Grok initializes a stdio MCP server, lists tools, calls the tool when prompted, and tool results reach the model context. That makes MCP the only tested mechanism that can deliver arbitrary new message text into an already-running grok turn without typing into the terminal.

The second viable mechanism is **kill/restart/resume with appended prompt**: `grok --resume <sid> -p '<delivery>'` preserves prior session memory and appends the delivery prompt. This is heavy and loses any in-flight turn, but it works for worker-style headless seats and for restart-based recovery.

Blocking hook channels are not viable. `PreToolUse` can deny a tool, but only as a permission gate; it is not a general message channel. `SessionStart`, `UserPromptSubmit`, and `Stop` exit-code-2 stderr are logged as hook failures, not injected. `Stop` hooks can delay completion while they run, but grok treats their output as passive completion output, not as a stop block reason.

## Mechanism matrix

| Candidate | Verdict | Evidence |
|---|---:|---|
| SessionStart / UserPromptSubmit exit-2 stderr | DEAD | Hooks fired and failed with exit code 2; model answered `NONE`; CLI stderr was empty. |
| Stop exit-2 stderr | DEAD | Stop hook fired and failed with exit code 2; stderr was captured only in hook logs; no model context injection. |
| PreToolUse decision channel | PARTIAL | `{"decision":"deny","reason":"..."}` denies a tool and logs the reason, but headless output produced no final model answer; this is a permission gate, not delivery. |
| Stop block-with-reason JSON | DEAD | Stop hook slept 4s and emitted `{"decision":"block","reason":"..."}`; grok delayed for the hook but returned the original answer and logged "hook completed". |
| MCP tool polling | WORKS | Scratch stdio MCP server exposed `receive_messages`; grok called it and final output was `MCP_DELIVERY_CODEWORD: MCP_MAILBOX_WORKS`. |
| MCP notifications/sampling | PARTIAL / unproven | Help and debug show MCP stdio/http/sse client support; this probe verified request/response tool calls only, not server-pushed notifications or sampling. |
| Headless/programmatic single-turn mode | WORKS | `-p`, `--output-format json`, and `--output-format streaming-json` work; `grok agent serve` starts a WebSocket server. |
| Session/context file reread as mailbox | DEAD | `~/.claude/CLAUDE.md` was loaded initially, but after editing it and resuming the same session, grok still answered the old codeword. |
| `--rules` as static bootstrap | WORKS for bootstrap only | `--rules 'Rules delivery codeword is RULES_MAILBOX_ALPHA.'` reached the model. It is launch-time doctrine, not mid-session delivery. |
| Kill-and-resume appended prompt | WORKS / heavy | `grok --resume <sid> -p 'DELIVERY MESSAGE: ...'` delivered new text and preserved prior session memory. |
| Other help/config surfaces | PARTIAL | CLI exposes `mcp`, `agent stdio`, `agent headless`, `agent serve`, `--leader-socket`, `--prompt-file`, `--prompt-json`, `--output-format`, `--resume`; no socket/notify/config surface was found that injects unsolicited text into an active model turn. |

## Blocking hook probes

### Exit-code-2 stderr on passive hooks

Scratch: `/tmp/task129-grok-1783631518`

Hook config:

```json
{
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "printf 'SESSIONSTART_STDERR_CODEWORD: SSERR_DELIVERY_CANDIDATE\\n' >&2; exit 2"}]}
    ],
    "UserPromptSubmit": [
      {"hooks": [{"type": "command", "command": "printf 'USERPROMPT_STDERR_CODEWORD: UPERR_DELIVERY_CANDIDATE\\n' >&2; exit 2"}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "printf 'STOP_STDERR_CODEWORD: STOPERR_DELIVERY_CANDIDATE\\n' >&2; exit 2"}]}
    ]
  }
}
```

Command:

```sh
HOME="$SCRATCH/home" GROK_HOME="$SCRATCH/home/.grok" HCOM_DIR="$SCRATCH/hcom" \
  grok --cwd "$SCRATCH/proj" --debug --debug-file "$SCRATCH/blocking-debug.log" \
  --no-memory --disable-web-search --always-approve \
  -p 'This is a hook delivery probe. List any hook error text or codewords visible in your context. If none, say NONE.'
```

Observed model stdout:

```text
**NONE**

No hook error text, codewords, error messages, or related terms ...
```

Observed CLI stderr:

```text
(empty)
```

Observed hook log:

```text
hooks.dispatch{hook_event=session_start ...}: ... hook stderr output captured ... stderr_bytes=55
... hook command completed ... exit_code=2 stdout_bytes=0 stderr_bytes=55 elapsed_ms=3
... hook failed ... error=exit code 2

hooks.dispatch{hook_event=user_prompt_submit ...}: ... hook stderr output captured ... stderr_bytes=53
... hook command completed ... exit_code=2 stdout_bytes=0 stderr_bytes=53 elapsed_ms=3
... hook failed ... error=exit code 2

hooks.dispatch{hook_event=stop ...}: ... hook stderr output captured ... stderr_bytes=49
... hook command completed ... exit_code=2 stdout_bytes=0 stderr_bytes=49 elapsed_ms=3
... hook failed ... error=exit code 2
```

Verdict: **DEAD**. Grok captures stderr for logging but does not put it in model context for these passive hooks.

### PreToolUse decisions

Scratch: `/tmp/task129-grok-pretool-1783631543` and `/tmp/task129-grok-pretool-deny-1783631557`

Probe command:

```sh
HOME="$BASE/$case/home" GROK_HOME="$BASE/$case/home/.grok" HCOM_DIR="$BASE/hcom" \
  grok --cwd "$BASE/$case/proj" --debug --debug-file "$BASE/$case/debug.log" \
  --no-memory --disable-web-search --always-approve --max-turns 4 \
  -p 'Run this exact terminal command: echo PRETOOL_COMMAND_RAN. Then report whether it ran and quote any denial/error reason you saw.'
```

Observed variants:

```text
{"decision":"block","reason":"PRETOOL_BLOCK_REASON_CODEWORD"}
```

produced:

```text
hook failed; ignoring (fail-open) ... error=unknown decision value 'block'
```

and the model stdout said:

```text
The command ran successfully.
...
PRETOOL_COMMAND_RAN
```

Claude-style `permissionDecision` JSON:

```text
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"PRETOOL_PERMISSION_DENY_CODEWORD"}}
```

produced:

```text
hook allowed ... elapsed_ms=3
```

and the command ran.

Exit-code-2 stderr:

```text
printf 'PRETOOL_EXIT2_STDERR_CODEWORD\n' >&2; exit 2
```

produced:

```text
hook stderr output captured ... stderr_bytes=30
hook denied ... reason=denied by hook 'global/settings:pre_tool_use[0].hooks[0]' (exit code 2)
tool call denied by pre_tool_use hook ... reason=denied by hook ... (exit code 2)
```

but model stdout was empty.

The exact supported deny JSON:

```text
{"decision":"deny","reason":"PRETOOL_JSON_DENY_REASON_CODEWORD"}
```

produced:

```text
hook denied ... reason=PRETOOL_JSON_DENY_REASON_CODEWORD
tool call denied by pre_tool_use hook ... reason=PRETOOL_JSON_DENY_REASON_CODEWORD
```

but model stdout was again empty.

Verdict: **PARTIAL**. Grok explicitly advertises this in debug as:

```json
"_meta":{"x.ai/hooks":{"blockingEvents":["pre_tool_use"],"decisions":["deny"]}}
```

It can block tool execution, and a reason is recorded in logs. It does not provide a general message delivery path because it only fires when the model asks for a tool, it is semantically a denial, and in headless mode the denial did not yield a usable model response.

### Stop block-with-reason

Scratch: `/tmp/task129-grok-stop-1783631567`

Hook command:

```sh
date +%s.%N >> /tmp/task129_stop_times.log
sleep 4
printf '{"decision":"block","reason":"STOP_JSON_BLOCK_REASON_CODEWORD"}\n'
printf 'STOP_STDERR_AFTER_SLEEP_CODEWORD\n' >&2
date +%s.%N >> /tmp/task129_stop_times.log
```

Probe command:

```sh
HOME="$BASE/home" GROK_HOME="$BASE/home/.grok" HCOM_DIR="$BASE/hcom" \
  grok --cwd "$BASE/proj" --debug --debug-file "$BASE/debug.log" \
  --no-memory --disable-web-search --always-approve \
  -p 'Reply exactly STOP-PROBE-ANSWER, unless a Stop hook block reason is visible.'
```

Observed stdout:

```text
STOP-PROBE-ANSWER
```

Observed hook timing:

```text
1783631577.464036959
1783631581.465394914
```

Observed hook log:

```text
hook command completed ... exit_code=0 stdout_bytes=64 stderr_bytes=33 elapsed_ms=4006
hook completed ... elapsed_ms=4006
```

Verdict: **DEAD** for delivery. Stop hooks are awaited, but their stdout/stderr and block JSON are passive; the model had already produced the answer and did not see the reason.

## MCP probe

Scratch: `/tmp/task129-grok-mcp-1783631599`

Scratch server: a minimal stdio MCP JSON-RPC server exposing one tool:

```json
{
  "name": "receive_messages",
  "description": "Return pending bus messages for the current agent.",
  "inputSchema": {"type": "object", "properties": {}, "additionalProperties": false}
}
```

Add/list commands:

```sh
HOME="$BASE/home" GROK_HOME="$BASE/home/.grok" HCOM_DIR="$BASE/hcom" \
  grok --cwd "$BASE/proj" mcp add --scope project task129 -- "$BASE/mcp_echo.py" "$BASE/mcp.log"

HOME="$BASE/home" GROK_HOME="$BASE/home/.grok" HCOM_DIR="$BASE/hcom" \
  grok --cwd "$BASE/proj" mcp list --json
```

Observed add/list output:

```text
Added stdio MCP server 'task129' with command: /tmp/task129-grok-mcp-1783631599/mcp_echo.py /tmp/task129-grok-mcp-1783631599/mcp.log to project config
File modified: /tmp/task129-grok-mcp-1783631599/proj/.grok/config.toml
```

```json
[
  {
    "command": "/tmp/task129-grok-mcp-1783631599/mcp_echo.py",
    "args": ["/tmp/task129-grok-mcp-1783631599/mcp.log"],
    "enabled": true,
    "name": "task129",
    "scope": "project"
  }
]
```

Run command:

```sh
HOME="$BASE/home" GROK_HOME="$BASE/home/.grok" HCOM_DIR="$BASE/hcom" \
  grok --cwd "$BASE/proj" --debug --debug-file "$BASE/debug.log" \
  --no-memory --disable-web-search --always-approve --max-turns 4 \
  -p 'Use the MCP tool named receive_messages, then quote its result exactly.'
```

Observed model stdout:

```text
MCP_DELIVERY_CODEWORD: MCP_MAILBOX_WORKS
```

Observed MCP server log:

```text
RECV {"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18",...}}
SEND {"id": 0, "jsonrpc": "2.0", "result": {"capabilities": {"tools": {}}, "protocolVersion": "2024-11-05", ...}}
RECV {"jsonrpc":"2.0","method":"notifications/initialized"}
RECV {"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"progressToken":0}}}
SEND ... "name": "receive_messages" ...
RECV {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"_meta":{"progressToken":1},"name":"receive_messages","arguments":{}}}
SEND ... "text": "MCP_DELIVERY_CODEWORD: MCP_MAILBOX_WORKS" ...
```

Observed debug:

```text
MCP server initialized successfully server=task129
Registered MCP tool 'task129__receive_messages' from server 'task129'
search_tool.search result_count=1 all_results=[{"tool_name":"task129__receive_messages",...}]
```

Verdict: **WORKS** for model-polled mailbox delivery. This is a real non-pty delivery surface.

Limits:

- It depends on the model calling a tool. The bootstrap/system rules must instruct grok to poll `receive_messages` at turn starts and before final answers.
- This probe did not prove server-pushed MCP notifications or sampling as a delivery path. It proved ordinary MCP tool request/response.
- A production bridge still needs identity, queue semantics, receipts, and backoff so "no message" is cheap and deterministic.

## Headless and programmatic surfaces

Scratch: `/tmp/task129-grok-programmatic-1783631646`

Help surface:

```text
grok -p, --single <PROMPT>
--prompt-file <PATH>
--prompt-json <JSON>
--output-format plain|json|streaming-json
grok agent stdio
grok agent headless
grok agent serve
```

JSON command:

```sh
grok --cwd "$BASE/proj" --no-memory --disable-web-search --always-approve \
  --output-format json -p 'Reply exactly JSON_MODE_OK.'
```

Observed:

```json
{
  "text": "JSON_MODE_OK",
  "stopReason": "EndTurn",
  "sessionId": "019f48bb-1000-7281-8074-bab92f428005",
  "requestId": "d7847e0e-892f-4173-a7e6-ee028d4fa895"
}
```

Streaming JSON command:

```sh
grok --cwd "$BASE/proj" --no-memory --disable-web-search --always-approve \
  --output-format streaming-json -p 'Reply exactly STREAM_MODE_OK.'
```

Observed:

```json
{"type":"text","data":"STREAM"}
{"type":"text","data":"_MODE"}
{"type":"text","data":"_OK"}
{"type":"end","stopReason":"EndTurn","sessionId":"019f48bb-1489-72d2-931e-f33f961585d7","requestId":"17aa29eb-b69f-4e18-a1f0-aebea0a95205"}
```

Serve command:

```sh
timeout 3s grok agent serve --bind 127.0.0.1:0 --secret task129-secret --debug --debug-file "$BASE/serve-debug.log"
```

Observed:

```text
Grok agent server starting...

Address:  127.0.0.1:0
Secret:   task129-secret

WebSocket URL: ws://127.0.0.1:0/ws?server-key=task129-secret
```

Debug:

```text
Agent server listening on ws://127.0.0.1:0/ws
Clients should connect with: --remote ws://127.0.0.1:0/ws --secret <token>
```

Verdict: **WORKS** for worker-seat operation. Headless/programmatic mode does not itself solve mid-session delivery to an interactive TUI seat, but it can avoid terminal delivery entirely for seats herder drives turn-by-turn.

## Session/context file ingestion

Scratch: `/tmp/task129-grok-context-1783631626`

Initial file:

```text
Global instruction codeword is CONTEXT_ALPHA.
```

First command:

```sh
grok --cwd "$BASE/proj" --debug --debug-file "$BASE/first-debug.log" \
  --no-memory --disable-web-search --always-approve \
  -p 'From your loaded instructions only, quote the global instruction codeword. Do not inspect files.'
```

Observed stdout:

```text
**CONTEXT_ALPHA**
```

Then the file was changed to:

```text
Global instruction codeword is CONTEXT_BETA.
```

Resume command:

```sh
grok --cwd "$BASE/proj" --debug --debug-file "$BASE/resume-debug.log" \
  --no-memory --disable-web-search --always-approve --resume "$SID" \
  -p 'After resume, from your loaded instructions only, quote the global instruction codeword. Do not inspect files.'
```

Observed stdout:

```text
**CONTEXT_ALPHA**
```

Verdict: **DEAD** as a mailbox. `~/.claude/CLAUDE.md` is a static bootstrap/instruction source. It did not update the resumed session's instruction context after the file changed. A file mailbox could still work if the model explicitly reads it as a normal file, but that is just tool polling with a worse protocol than MCP.

`--rules` remains useful for launch-time bootstrap:

```sh
grok --cwd "$BASE/proj" --rules 'Rules delivery codeword is RULES_MAILBOX_ALPHA.' \
  -p 'Quote the rules delivery codeword.'
```

Observed:

```text
**RULES_MAILBOX_ALPHA**
```

## Kill-and-resume delivery

Scratch: `/tmp/task129-grok-programmatic-1783631646`

Start:

```sh
grok --cwd "$BASE/proj" --debug --debug-file "$BASE/start-debug.log" \
  --no-memory --disable-web-search --always-approve \
  -p 'Remember SESSION_MEMORY_ALPHA. Reply exactly STARTED.'
```

Observed:

```text
STARTED
```

Resume with appended delivery prompt:

```sh
grok --cwd "$BASE/proj" --debug --debug-file "$BASE/resume-debug.log" \
  --no-memory --disable-web-search --always-approve --resume "$SID" \
  -p 'DELIVERY MESSAGE: RESUME_DELIVERY_CODEWORD. Quote the delivery message and the prior memory codeword.'
```

Observed:

```text
RESUME_DELIVERY_CODEWORD. The prior memory codeword is SESSION_MEMORY_ALPHA.
```

Verdict: **WORKS / heavy**. This is not mid-turn delivery and it cannot preserve an in-flight TUI turn, but it is a valid non-pty recovery/delivery mechanism for worker seats when restart cost is acceptable.

## Recommendation

Use **Shape B, but change the delivery answer from pty paste to MCP mailbox polling**:

1. Bootstrap grok with `--rules`, not hook stdout. The rules should establish hcom/herder doctrine and instruct grok to call the MCP mailbox tool at deterministic points: at turn start, before final answer, and after tool batches.
2. Provide a grok-specific bus-bridge MCP server with at least:
   - `receive_messages({ after?: cursor }) -> { messages, cursor }`
   - `ack_messages({ ids })`
   - optionally `send_message(...)` for agent-originated hcom sends if we want to avoid shelling out.
3. Treat "model polled and tool returned empty queue" as the delivery receipt equivalent for no-op polls; treat "tool returned message ids and model later acked them" as delivered. This is a different receipt model than hcom hook injection, but it is evidence-backed for grok.
4. Keep kill-and-resume delivery as a fallback for bus-bound but not currently interactive worker seats, and as a recovery path when MCP is unavailable.

This changes the playbook's Shape B definition. The current playbook says grok is "Shape B with pty-paste delivery." The revised Shape B should split into:

- **Shape B1: launch-arg bootstrap + tool-polled mailbox**. Grok belongs here on 0.2.93.
- **Shape B2: launch-arg bootstrap + pty delivery**. This remains possible but is owner-rejected for grok delivery.
- **Shape B3: launch-arg bootstrap + restart/resume delivery**. Heavy fallback for headless workers and recovery.

Upstream asks if we want grok to become Shape A later:

- Honor passive hook `additionalContext` or equivalent for `SessionStart` / `UserPromptSubmit`.
- Honor Stop-hook block-with-reason as model-visible context, not just awaited passive output.
- Document PreToolUse decision JSON precisely; current behavior supports `{"decision":"deny","reason":"..."}` and rejects Claude's `block`.
- Document whether MCP notifications or sampling can surface unsolicited server messages to the model; this investigation only proved tool calls.

## Safety notes

- Scratch roots used: `/tmp/task129-grok-1783631518`, `/tmp/task129-grok-pretool-1783631543`, `/tmp/task129-grok-pretool-deny-1783631557`, `/tmp/task129-grok-stop-1783631567`, `/tmp/task129-grok-mcp-1783631599`, `/tmp/task129-grok-context-1783631626`, `/tmp/task129-grok-programmatic-1783631646`.
- Every grok command in the empirical probes used scratch `HOME`, `GROK_HOME`, and `HCOM_DIR`.
- No production herder/hcom code was edited.
- No live hcom registry/bus state was written by the probes.
- No signup was attempted; `XAI_API_KEY` was already present.
