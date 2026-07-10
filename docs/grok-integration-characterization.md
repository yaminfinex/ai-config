# Grok integration characterization

Date: 2026-07-10
Sources: initial CLI characterization and follow-up delivery probes
Subject: xAI Grok CLI 0.2.93 (`f00f96316d`)
Status: current characterization; investigation only, no production code changed

## Decision

Grok can be a long-lived herder worker, but it is not Claude-compatible at the delivery boundary even though it runs Claude-compatible hooks.

Use this integration shape:

1. Launch Grok with a preassigned session id, pinned `GROK_HOME`, and bootstrap doctrine in `--rules`.
2. Start one persistent Grok `monitor` whose command blocks on the worker's hcom queue and prints exactly one compact line per deliverable message.
3. Use MCP for structured message fetch, acknowledgement, recovery, and replies.
4. Keep `--resume <session-id> -p '<delivery>'` as a heavy recovery path.

Do not use passive hook output, instruction-file mutation, semantic MCP polling, or terminal paste as the primary wake-up channel.

The important monitor behavior is:

- while idle, a line wakes Grok and creates a synthetic notification turn;
- while a turn is active, a line is buffered until the turn reaches a boundary, then injected into that same turn;
- it does not cancel or preempt an active foreground tool.

## Recommended architecture

```text
hcom queue
    |
    v
selective bridge process -- one compact line per message
    |
    v
Grok persistent monitor -- wake + context injection
    |
    +--> MCP fetch_message(id) -- full payload
    +--> MCP ack_message(id)   -- delivery receipt
    +--> MCP send_message(...) -- worker reply
    +--> MCP list_pending()    -- reconnect recovery
```

The bridge must stay silent when there is no message. Logs, heartbeats, stack traces, and diagnostics must go to a separate file because every stdout or stderr line becomes a conversation notification.

A compact monitor payload can carry only routing data:

```text
HCOM_DELIVERY id=33758 from=hera intent=request thread=grokprobe hash=...
```

The model then fetches the full payload over MCP and acknowledges it after processing. This avoids shell quoting and line-size problems while retaining push-like wake-up behavior.

## Capability matrix

| Surface | Verdict | Integration relevance |
|---|---:|---|
| Persistent `monitor` | **WORKS** | Primary wake and injection channel. Wakes idle sessions and buffers mid-turn events to a turn boundary. |
| MCP tool calls | **WORKS** | Structured fetch, ack, recovery, and reply channel. Requires the model to call a tool. |
| `--rules` | **WORKS** | Launch-time bootstrap doctrine. Not a dynamic mailbox. |
| `--resume <sid> -p ...` | **WORKS / heavy** | Preserves prior context and appends delivery; suitable for recovery, not active-turn delivery. |
| Interactive pane prompt | **WORKS / rejected** | Herder can paste at Grok's `❯` composer, but terminal injection is not the desired bus architecture. |
| Headless JSON / streaming JSON | **WORKS** | Suitable for worker seats driven one turn at a time. |
| `grok agent serve` | **WORKS / uncharacterized** | WebSocket server starts, but monitor delivery over this API was not tested. |
| Claude-compatible status hooks | **PARTIAL** | Lifecycle and tool hooks fire, but payloads and delivery semantics differ from Claude. |
| Passive hook stdout/stderr | **DEAD** | Captured in logs and discarded from model context. |
| `PreToolUse` denial | **PARTIAL** | Can deny a tool with a reason; it is a permission gate, not general delivery. |
| Stop-hook blocking output | **DEAD** | Grok waits for the hook but does not inject its result or block reason into model context. |
| Instruction-file mutation | **DEAD** | A resumed session retained the original instruction snapshot. |
| Scheduler / `/loop` | **PARTIAL** | Useful for periodic prompts at intervals of at least 60 seconds, not event delivery. |
| Background command polling | **PARTIAL** | Useful for builds and servers; not a selective bus stream. |

## CLI and session surface

### Launch

Verified launch capabilities:

- interactive TUI and headless `-p` / `--single` modes;
- `--output-format plain|json|streaming-json`;
- `--session-id <uuid>` for a new, preassigned session;
- `--resume <session-id>` and `--fork-session`;
- `--always-approve` and `--permission-mode ...`;
- `GROK_HOME` for config-directory pinning;
- `GROK_AGENT=1` in shell children for self-detection;
- `--no-alt-screen --minimal` for scrollback-native TUI rendering.

The TUI composer sigil is `❯`, the same as Claude. Literal text followed by a separate Enter was reliable; startup modals can consume the first paste.

### Session files

Sessions live under:

```text
~/.grok/sessions/<url-encoded-cwd>/<session-id>/
```

Important files:

- `chat_history.jsonl`: complete model transcript and the best observer source;
- `events.jsonl`: lifecycle, phase, tool, and permission events with explicit turn numbers;
- `updates.jsonl`: streamed ACP updates and the path advertised by later hooks;
- `system_prompt.txt`, `prompt_context.json`, `summary.json`, and `rewind_points.jsonl`.

`events.jsonl` phase changes and the TUI's `Turn completed in ...` output provide clean turn-boundary signals. A session id is also printed at exit and can be listed or exported with Grok's session commands.

## Hook compatibility

Grok loads global Claude hooks by default and project hooks after folder trust is granted. Project hooks require a git worktree root. Hooks also fire in headless mode.

### Event census

| Claude event | Grok behavior |
|---|---|
| `SessionStart` | Fires lazily at the first prompt and in headless mode. |
| `UserPromptSubmit` | Fires. |
| `PreToolUse` / `PostToolUse` | Fire; Claude tool-name aliases can match Grok tools. |
| `Stop` | Fires at every turn end. |
| `Notification` | Fires. |
| `SubagentStart` / `SubagentStop` | Fire for distinct subagent sessions. |
| `SessionEnd` | Seen for subagents, not observed on primary TUI `/quit`. |
| `PermissionRequest` | Does not fire; Grok has a different permission event. |
| `PreCompact` / `PostCompact` | Documented but not exercised. |

Payloads are Grok-shaped: camelCase keys such as `hookEventName`, `sessionId`, `workspaceRoot`, and `transcriptPath`, with snake_case event values. `transcriptPath` is absent on a new `SessionStart` and appears later, pointing to `updates.jsonl` rather than `chat_history.jsonl`.

### Why hooks cannot deliver messages

Three independent probes reached the same result:

- plain SessionStart output and Claude-style `additionalContext` were not visible to the model;
- exit-code-2 stderr from SessionStart, UserPromptSubmit, and Stop was logged but not injected;
- a Stop hook that slept four seconds and emitted `{"decision":"block","reason":"..."}` delayed completion but did not change the answer.

`PreToolUse` supports Grok's exact denial shape:

```json
{"decision":"deny","reason":"..."}
```

It can stop a requested tool and record a reason, but it only runs at tool-request time and is semantically a permission denial. It is not a mailbox.

## Existing hcom interaction

A plain Grok process currently auto-runs the globally configured hcom Claude hooks. This produces a misleading half-integration:

- Grok registers and emits useful lifecycle/status events;
- hcom labels it as `tool: claude`;
- the SessionStart bootstrap is discarded, so Grok does not learn its bus identity;
- hcom's Stop-hook listener exits instead of delivering context;
- `hcom term inject` has no port because hcom did not launch Grok's PTY.

Two identity hazards were observed:

- identity appeared directory-keyed, allowing a later Grok session in the same cwd to claim an existing identity;
- a subagent's `SessionEnd` marked the parent hcom instance stopped.

Production support should either disable this accidental compatibility path or teach hcom to identify and handle Grok explicitly. Treating it as Claude is unsafe.

## Delivery evidence

### Persistent monitor: idle worker

A headless worker started:

```json
{
  "name": "monitor",
  "arguments": {
    "command": "tail -n0 -F mailbox.log",
    "description": "hcom-monitor-bridge",
    "timeout_ms": null,
    "persistent": true
  }
}
```

Appending a line externally produced a synthetic transcript entry:

```xml
<monitor-event task_id="019f496c-ed50-7a92-a861-0d44da6d19bd">
[hcom-monitor-bridge] HCOM_MONITOR_DELIVERY id=42 from=hera text=MONITOR_WAKE_CODEWORD
</monitor-event>
```

The entry had `synthetic_reason: "notification_drain"`, received a `notifications-...` prompt id, and caused an automatic assistant response. A second appended line caused a second notification turn. No terminal input or model polling was involved.

One-shot monitors also merge stdout and stderr and emit lines as notifications. The reliable receipt is the later synthetic notification turn, not the model's immediate answer after starting the monitor.

### Persistent monitor: interactive TUI

The same `tail -n0 -F mailbox.log` monitor remained active in a real Herdr side pane.

While idle, an appended `HCOM_TUI_IDLE id=101 ...` line produced a `notification_drain` user entry and an automatic reply.

During a five-second foreground command, another line arrived after `BUSY_STEP_2`. The debug and transcript sequence was:

```text
00:50:23.374 Monitor event received, injecting into session
00:50:23.374 Routed monitor event to mid-turn buffer
00:50:23.665 BUSY_STEP_3
00:50:24.665 BUSY_STEP_4
00:50:25.665 BUSY_STEP_5
00:50:26.671 injected mid-turn monitor events as hidden synthetic user message
00:50:27.799 assistant response included the event and command summary
```

The busy event had `synthetic_reason: "system_reminder"`. Grok did not cancel the command or start another assistant turn. Monitor delivery is therefore queued to a turn boundary, not a hard interrupt.

### MCP

A scratch stdio MCP server exposed:

```json
{
  "name": "receive_messages",
  "description": "Return pending bus messages for the current agent.",
  "inputSchema": {"type":"object","properties":{},"additionalProperties":false}
}
```

Grok initialized the server, listed the tool, called it, and returned the exact tool result:

```text
MCP_DELIVERY_CODEWORD: MCP_MAILBOX_WORKS
```

This proves request/response MCP tool delivery. It does not prove unsolicited MCP notifications or sampling. MCP alone is a semantic polling mechanism, so it is better used behind monitor wake-up than as the primary notification path.

### Other mechanisms

- `--rules` reached model context and is the clean bootstrap path.
- Updating `~/.claude/CLAUDE.md` after a session started did not update that session on resume.
- `--resume <sid> -p '<delivery>'` delivered new text while retaining a prior memory codeword.
- JSON and streaming-JSON headless modes returned stable session and request ids.
- `grok agent serve` started a WebSocket endpoint, but no client protocol or monitor propagation probe was run.
- `/loop` and `scheduler_create` are real, but the 60-second interval floor makes them unsuitable for low-latency bus delivery.

## Integration work

The implementation should cover these boundaries:

| Area | Required change |
|---|---|
| Agent recognition | Add Grok as an explicit agent type rather than relying on Claude hook compatibility. |
| Launch | Pin `GROK_HOME`, preassign a session id, apply the requested permission mode, and pass bootstrap doctrine through `--rules`. |
| Monitor bridge | Provide a per-agent blocking queue reader that emits one compact line per message and no incidental output. |
| MCP bridge | Expose fetch, ack, send, and pending/recovery operations with explicit cursors or message ids. |
| Lifecycle | Detect `GROK_AGENT=1`; support `--resume` and `--fork-session`; reinstall the monitor if resume does not preserve it. |
| Observer | Resolve Grok's session directory and read `chat_history.jsonl`; do not treat hook `transcriptPath` as the full transcript. |
| Receipts | Define delivery as monitor injection plus MCP acknowledgement, not as Claude hook delivery. |
| Subagents | Prevent child lifecycle events from stopping or stealing the parent worker identity. |

Recommended delivery taxonomy:

- **B1: `--rules` bootstrap + monitor wake + MCP fetch/ack**: preferred for Grok 0.2.93.
- **B2: `--rules` bootstrap + MCP polling only**: functional fallback with model-driven latency.
- **B3: `--rules` bootstrap + restart/resume delivery**: heavy recovery path.
- **B4: `--rules` bootstrap + PTY paste**: technically functional, but rejected as the primary design.

## Open questions

1. Does `grok agent serve` propagate monitor notifications with the same synthetic-turn semantics?
2. Does a persistent monitor survive compaction and resume, or must herder reinstall it?
3. What rate or volume causes Grok to auto-stop a noisy monitor?
4. Should monitor lines contain a complete small message or only an id and integrity hash?
5. How should hcom expose a blocking per-agent stream without also printing operational diagnostics?

## Probe inventory and safety

The consolidated findings came from isolated scratch roots under `$SCRATCHPAD/grokchar/` and `/tmp/grokprobe-grok-*`, including hook, MCP, context, programmatic, scheduler, headless-monitor, idle-monitor, and interactive-TUI probes.

All delivery probes used scratch `HOME`, `GROK_HOME`, and `HCOM_DIR`. The initial CLI characterization used a private tmux server and isolated hcom bus. No live registry or production bus state was modified, no production herder/hcom code was changed, and no signup was attempted. The pre-existing `XAI_API_KEY` was used. A temporary Grok folder-trust entry created during characterization was removed afterward.

## Investigation chronology

The recommendations evolved as progressively stronger surfaces were tested:

1. CLI characterization proved that Grok could launch, resume, expose transcripts, and accept pane prompts, but passive Claude-hook output could not deliver context.
2. The non-PTY matrix proved MCP tool delivery and restart/resume delivery; MCP polling became the provisional recommendation.
3. Background-task probes proved that persistent monitors wake idle workers and buffer busy-worker events. The final recommendation is monitor wake plus MCP fetch and acknowledgement.

---

## Full delivery mechanism evidence

Date: 2026-07-09
Author: research worker
Subject: xAI grok CLI 0.2.93 (`grok 0.2.93 (f00f96316d)`, installed at `~/.local/bin/grok`)
Unit type: investigate only; no herder/hcom production code changed.

The full initial CLI evidence appendix below already proves that passive hook stdout is discarded and that pty paste works but is unacceptable as the desired delivery mechanism. The question here is narrower: does grok expose any other mid-session delivery surface that can replace pty paste?

All empirical runs used scratch directories under `/tmp/grokprobe-grok-*`, scratch `HOME`, scratch `GROK_HOME`, and scratch `HCOM_DIR`. No live registry or live hcom bus state was used. The only pre-existing credential used was `XAI_API_KEY`; no signup or account flow was attempted.

### Summary

The best non-pty delivery mechanism in grok 0.2.93 is **MCP tool polling**.

A bus-bridge MCP server can expose a `receive_messages` tool. Grok initializes a stdio MCP server, lists tools, calls the tool when prompted, and tool results reach the model context. That makes MCP the only tested mechanism that can deliver arbitrary new message text into an already-running grok turn without typing into the terminal.

The second viable mechanism is **kill/restart/resume with appended prompt**: `grok --resume <sid> -p '<delivery>'` preserves prior session memory and appends the delivery prompt. This is heavy and loses any in-flight turn, but it works for worker-style headless seats and for restart-based recovery.

Blocking hook channels are not viable. `PreToolUse` can deny a tool, but only as a permission gate; it is not a general message channel. `SessionStart`, `UserPromptSubmit`, and `Stop` exit-code-2 stderr are logged as hook failures, not injected. `Stop` hooks can delay completion while they run, but grok treats their output as passive completion output, not as a stop block reason.

### Mechanism matrix

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

### Blocking hook probes

#### Exit-code-2 stderr on passive hooks

Scratch: `/tmp/grokprobe-grok-1783631518`

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

#### PreToolUse decisions

Scratch: `/tmp/grokprobe-grok-pretool-1783631543` and `/tmp/grokprobe-grok-pretool-deny-1783631557`

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

#### Stop block-with-reason

Scratch: `/tmp/grokprobe-grok-stop-1783631567`

Hook command:

```sh
date +%s.%N >> /tmp/grokprobe_stop_times.log
sleep 4
printf '{"decision":"block","reason":"STOP_JSON_BLOCK_REASON_CODEWORD"}\n'
printf 'STOP_STDERR_AFTER_SLEEP_CODEWORD\n' >&2
date +%s.%N >> /tmp/grokprobe_stop_times.log
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

### MCP probe

Scratch: `/tmp/grokprobe-grok-mcp-1783631599`

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
  grok --cwd "$BASE/proj" mcp add --scope project grokprobe -- "$BASE/mcp_echo.py" "$BASE/mcp.log"

HOME="$BASE/home" GROK_HOME="$BASE/home/.grok" HCOM_DIR="$BASE/hcom" \
  grok --cwd "$BASE/proj" mcp list --json
```

Observed add/list output:

```text
Added stdio MCP server 'grokprobe' with command: /tmp/grokprobe-grok-mcp-1783631599/mcp_echo.py /tmp/grokprobe-grok-mcp-1783631599/mcp.log to project config
File modified: /tmp/grokprobe-grok-mcp-1783631599/proj/.grok/config.toml
```

```json
[
  {
    "command": "/tmp/grokprobe-grok-mcp-1783631599/mcp_echo.py",
    "args": ["/tmp/grokprobe-grok-mcp-1783631599/mcp.log"],
    "enabled": true,
    "name": "grokprobe",
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
MCP server initialized successfully server=grokprobe
Registered MCP tool 'grokprobe__receive_messages' from server 'grokprobe'
search_tool.search result_count=1 all_results=[{"tool_name":"grokprobe__receive_messages",...}]
```

Verdict: **WORKS** for model-polled mailbox delivery. This is a real non-pty delivery surface.

Limits:

- It depends on the model calling a tool. The bootstrap/system rules must instruct grok to poll `receive_messages` at turn starts and before final answers.
- This probe did not prove server-pushed MCP notifications or sampling as a delivery path. It proved ordinary MCP tool request/response.
- A production bridge still needs identity, queue semantics, receipts, and backoff so "no message" is cheap and deterministic.

### Headless and programmatic surfaces

Scratch: `/tmp/grokprobe-grok-programmatic-1783631646`

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
timeout 3s grok agent serve --bind 127.0.0.1:0 --secret grokprobe-secret --debug --debug-file "$BASE/serve-debug.log"
```

Observed:

```text
Grok agent server starting...

Address:  127.0.0.1:0
Secret:   grokprobe-secret

WebSocket URL: ws://127.0.0.1:0/ws?server-key=grokprobe-secret
```

Debug:

```text
Agent server listening on ws://127.0.0.1:0/ws
Clients should connect with: --remote ws://127.0.0.1:0/ws --secret <token>
```

Verdict: **WORKS** for worker-seat operation. Headless/programmatic mode does not itself solve mid-session delivery to an interactive TUI seat, but it can avoid terminal delivery entirely for seats herder drives turn-by-turn.

### Session/context file ingestion

Scratch: `/tmp/grokprobe-grok-context-1783631626`

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

### Kill-and-resume delivery

Scratch: `/tmp/grokprobe-grok-programmatic-1783631646`

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

### Recommendation

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

### Safety notes

- Scratch roots used: `/tmp/grokprobe-grok-1783631518`, `/tmp/grokprobe-grok-pretool-1783631543`, `/tmp/grokprobe-grok-pretool-deny-1783631557`, `/tmp/grokprobe-grok-stop-1783631567`, `/tmp/grokprobe-grok-mcp-1783631599`, `/tmp/grokprobe-grok-context-1783631626`, `/tmp/grokprobe-grok-programmatic-1783631646`.
- Every grok command in the empirical probes used scratch `HOME`, `GROK_HOME`, and `HCOM_DIR`.
- No production herder/hcom code was edited.
- No live hcom registry/bus state was written by the probes.
- No signup was attempted; `XAI_API_KEY` was already present.

---

## Full initial CLI characterization evidence

This appendix is a non-normative historical evidence record retained so the canonical characterization preserves the commands, observations, and safety notes behind its conclusions.

Date: 2026-07-09 · Author: research worker · Unit type: investigate (report only — no herder/hcom code changed)

Subject: xAI **grok CLI 0.2.93** (`grok 0.2.93 (f00f96316d)`, installed at `~/.local/bin/grok`), tested against the machine's already-configured `XAI_API_KEY`. All experiments ran in a scratch directory (`$SCRATCHPAD/grokchar/proj`) on a private tmux server (`tmux -L grokchar`) with an **isolated hcom bus** (`HCOM_DIR=$SCRATCHPAD/grokchar/.hcom`). The live registry/bus and all production code were untouched (see "Safety-rail compliance" at the end).

Companion doc: `docs/new-harness-onboarding.md` (the generalized checklist, with grok as worked example).

---

### Executive summary

- **Owner's hypothesis ("may Just Work"): NO — half works.** A plain grok session **does auto-join the hcom bus** through grok's always-on Claude-hooks compatibility (registers, gets a name, session id captured, full status tracking). But it **cannot receive a delivered message**: grok discards all passive-hook stdout, so hcom's context-injection delivery (and its bootstrap) never reaches the model. The specific missing capability is a **hook-output context-injection seam** (Claude's `additionalContext` / stop-block semantics).
- Everything else herder needs exists: pre-assignable session id (`--session-id`), on-disk turn events, working resume/fork flags, pane-paste prompt injection (composer sigil `❯`, same as claude), `--always-approve` danger mode, `GROK_HOME` config-dir pin, `GROK_AGENT=1` self-detect env.
- Viable integration shape: **codex-style** (bootstrap via launch args — grok's `--rules` appends to the system prompt) plus **pty-paste delivery** instead of hook injection. That is an hcom-side design change, not just switch arms.

---

### 1. Launch mechanics

Evidence run (headless sanity, scratch dir):

```
$ grok -p 'Reply with exactly: GROK-HEADLESS-OK'
GROK-HEADLESS-OK
```

- **Interactive**: `grok` starts an alt-screen TUI in the cwd (`--cwd <dir>` to point elsewhere; `--no-alt-screen`, `--minimal` for scrollback-native rendering). Launched fine inside a tmux pane; footer shows `grok-4.5 · always-approve`.
- **Headless**: `grok -p/--single '<prompt>'` (single turn, prints to stdout), `--output-format plain|json|streaming-json`, `--json-schema`, `grok agent` subcommand for non-interactive operation. **Hooks fire in headless mode too** (verified: a `-p` run dispatched `session_start`/`user_prompt_submit`/`stop` and self-registered on the isolated bus as `niru`).
- **Permissions**: `--permission-mode default|acceptEdits|auto|dontAsk|bypassPermissions|plan`, `--always-approve` (auto-approve all tools), `--allow/--deny` rules (help text explicitly maps them: "Permission allow rule (Claude Code: --allowedTools)"). Global default can come from `~/.grok/config.toml` (`[ui] permission_mode`).
- **Session identity at spawn**: `--session-id <uuid>` names a NEW session (must be a valid, unused UUID). **Verified**: launched with a generated UUID; hcom's registration recorded exactly that id (`session_id: e7245d83-cbdd-419e-93f9-ec5f892bd77d` == the `--session-id` value).
- **Config/auth**: `~/.grok/config.toml` + `XAI_API_KEY` (already configured here — no account blocker). `GROK_HOME` env var relocates the config dir (documented in user-guide 05/14/17). `--leader-socket` for the leader process; `GROK_SANDBOX` for sandbox profiles.
- **Trust gate**: project-local config (hooks/MCP/LSP) is gated by folder trust (`~/.grok/trusted_folders.toml`, `/hooks-trust` in the TUI). See §2 for the git-root surprise.

### 2. Claude-hooks compatibility mode

**How it's enabled: on by default.** Grok scans `~/.claude/settings.json` (+ `settings.local.json`) as an always-trusted global hook source, and `<project>/.claude/settings.json` as a trust-gated project source (disable via `[compat.claude] hooks = false`). Consequence on this machine: **every grok session loads hcom's Claude hooks and calls `hcom sessionstart` / `poll` / etc. — grok sessions self-join whatever bus `HCOM_DIR` points at**, with no grok-specific code anywhere.

Two loading gotchas (both empirical):

1. **Project hooks require a git worktree root.** In a non-git scratch dir, `/hooks-trust` refused: `Not in a git repository. Project hooks require a git worktree root.` After `git init` + `/hooks-trust` (`Trusted: …/grokchar/proj/.`), project `.claude/settings.json` hooks loaded and fired.
2. `GROK_FOLDER_TRUST=0` does **not** ungate project hooks (they simply never loaded; `hooks.dispatch … hook_count=1` = global only). Trust must be granted (`/hooks-trust` or `--trust`).

#### Event census (empirical)

Method: a project `.claude/settings.json` registering a logger hook (appends `$GROK_HOOK_EVENT` + raw stdin to a TSV) for 10 Claude event names, plus grok's debug log (`RUST_LOG=debug GROK_LOG_FILE=…`), across interactive turns, a subagent run, `/quit`, and a headless run.

| Claude hook event | Fires in grok? | Evidence |
|---|---|---|
| SessionStart | **YES** (lazily at first prompt / hooks reload, not at TUI startup; also headless) | hooklog `07:01:28 session_start`; debug `hook command completed hook_name=global/settings:session_start[0] … stdout_bytes=4556` |
| UserPromptSubmit | **YES** | hooklog ×3; TUI annotation `◆ user_prompt_submit [hooks: 3]` |
| PreToolUse | **YES**, matcher + Claude tool-name aliases work (`Bash\|Task\|Write\|Edit` matched `run_terminal_command`) | hooklog ×3 with `"toolName":"run_terminal_command","toolInput":{"command":"echo census-turn",…}` |
| PostToolUse | **YES** | hooklog ×3 |
| Stop | **YES**, at every turn end | hooklog ×4; TUI `◆ stop [hooks: 2] / Turn completed in 5.3s.` |
| Notification | **YES** | hooklog + debug log ×4+ |
| SubagentStart / SubagentStop | **YES** | debug `hook command completed hook_name=global/settings:subagent_start[0].hooks[0] exit_code=0` |
| SessionEnd | **PARTIAL** — fired when a *subagent's* session ended; **not observed on TUI `/quit`** of a primary session | hooklog `07:02:59 session_end` (subagent sid); no `session_end` dispatch in debug log after `/quit` of session 1 |
| PermissionRequest (used by hcom) | **NO** — not a grok event; silently skipped (grok has `PermissionDenied` instead) | never dispatched in either debug log |
| PreCompact | not exercised (no compaction occurred); documented by grok, with `PostCompact` added | grok user-guide 10-hooks.md |

**Subagent hazard**: a subagent runs as its own session (own `sessionId`, uuid-v7) and fires the *standard* hook set — its `session_end` caused hcom to mark the whole registered instance stopped (`exit:shutdown … life stopped by session`).

#### Payload format: grok-shaped, not Claude-shaped

camelCase keys, snake_case event values; no Claude `hook_event_name`/`session_id`/`transcript_path` keys. hcom 0.7.23 evidently tolerates this for registration/status, but any strict Claude-schema consumer would not. Sample (logged verbatim):

```json
{"hookEventName":"session_start","sessionId":"019f45ae-7352-76d0-9494-291d6b3fdc5d",
 "cwd":"…/grokchar/proj","workspaceRoot":"…/grokchar/proj/",
 "timestamp":"2026-07-09T07:01:28.582470804+00:00","source":"new"}
```

`transcriptPath` is **absent on `session_start`** (`source:"new"` — file not yet created) and present on all later events, pointing at `~/.grok/sessions/<urlenc-cwd>/<sid>/updates.jsonl`. That is why hcom registered the agent with `transcript: (none)`.

Hook env: `GROK_HOOK_EVENT`, `GROK_SESSION_ID`, `GROK_WORKSPACE_ROOT`, plus `CLAUDE_PROJECT_DIR` as a Claude-compat alias.

#### Context injection: **does not work — the deal-breaker**

Test: SessionStart hook echoing a plain-text codeword (`BLUEFERN`), UserPromptSubmit hook emitting Claude's canonical JSON (`{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"… REDMAPLE."}}`), then asking the model what codewords it knows. Model (with both hooks verifiably fired):

> "1) Codewords from context — From this session, the only codeword-like string is census-turn … Nothing else in the provided context looks like a planted codeword list. 2) Hcom agent name — I was not told an hcom agent name…"

Corroboration: hcom's own `sessionstart` bootstrap emitted **4,556 bytes of stdout** (debug log) — all discarded; the model never learned its bus identity. Grok's docs state it plainly: *"Passive Hooks — For events like SessionStart or PostToolUse, stdout is ignored."* Only `PreToolUse` output is honored, and only as allow/deny.

#### Blocking delivery: **does not work**

On Claude, hcom's Stop hook (`hcom poll`, `timeout: 86400`) blocks as the message listener. On grok, the same hook **completed in 32–36 ms** (`hook command completed … stop[0] … elapsed_ms=36`) — hcom self-exits (fail-open) rather than blocking, so ~30–60 s later the instance goes `inactive: stale` and `hcom send` refuses it: `@mentions to non-existent or stopped agents … @lara`. (Because hcom exited on its own, whether grok would honor the 86400 s `timeout` field is untested.)

### 3. Session / transcript behavior

Layout: `~/.grok/sessions/<url-encoded-cwd>/<session-uuid-v7>/` containing:

- `chat_history.jsonl` — full transcript (system prompt, messages) — the file an observer/transcript reader wants;
- `events.jsonl` — machine-readable lifecycle: `turn_started`, `loop_started`, `phase_changed` (`waiting_for_model`/`tool_execution`/`permission_prompt`), `tool_started/completed`, `permission_requested/resolved`, with `session_id`, `turn_number`, `schema_version:"1.0"` — a clean **turn-boundary signal on disk**;
- `updates.jsonl` (what hooks report as `transcriptPath`), `prompt_context.json`, `system_prompt.txt`, `summary.json`, `resources_state.json`, `rewind_points.jsonl`, `terminal/`.

Session id: uuid-v7, pre-assignable via `--session-id`, echoed in every hook payload and debug-log span, and printed at exit (`Resume this session with: grok --resume e7245d83-…`). `prompt_history.jsonl` sits per-cwd next to the session dirs; `grok sessions` lists/searches; `grok export` dumps a Markdown transcript.

Notable compat quirk: `prompt_context.json` shows grok ingests `~/.claude/CLAUDE.md` as an agents-md file — a static bootstrap surface that exists today (but is global/static, not per-spawn).

### 4. Prompt injection & turn-end detection

- **Pane paste works** (the herder path): every prompt in this characterization was delivered via `tmux send-keys -l '<text>'` + `Enter` into the TUI — plain prompts and slash commands. Composer sigil is **`❯` — identical to claude's**, so herder's bootpaste composer-state checks transfer. Hazards observed: a startup modal can swallow the first paste (Escape clears it — herder's modal-clear logic applies), and typing + `Enter` needs a brief gap (the `-l` literal + separate `Enter` pattern was reliable).
- **hcom pty injection does NOT work today**: `hcom term inject <name> … --enter` → `No inject port for 'niru'. Instance not running or not PTY-managed.` The inject port only exists when hcom itself spawns the tool's pty, and hcom has no grok launcher (its tool list: claude|gemini|codex|opencode|kilo|pi|omp|antigravity|cursor|kimi|copilot — no grok).
- **Turn-end signals** (for verified delivery): (a) the Stop hook fires reliably at each turn end — hcom already turns that into `status: listening` bus events for grok sessions (observed: `prompt → tool: → listening` flow); (b) `events.jsonl` `phase_changed`/turn records on disk; (c) TUI prints `Turn completed in Xs`. What's missing is not the signal but the delivery: herder's `DeliverBus` verification polls hcom `deliver:` receipts, which never come because hcom never injects into grok.

### 5. Bus-join hypothesis — definitive answer, and identity hazards

**Register: YES.** Reproduced flow (isolated bus): plain `grok` TUI in a scratch dir → first prompt → global Claude-compat hcom hooks fire → `hcom list` shows a named agent:

```
[claude*] lara   … session_id: e7245d83-cbdd-419e-93f9-ec5f892bd77d
          tool: claude   bindings: hooks, pty   transcript: (none)
```

with live status events (`start/listening → prompt/active → tool:/active → /listening`) and a lifecycle stop on exit. Note it registers as **`tool: claude`** — hcom cannot tell grok apart.

**Receive a delivered message: NO.** Missing capability, precisely: **grok's hook runner discards passive-hook stdout and has no blocking/turn-injection semantics**, so both of hcom's delivery paths (Stop-hook long-poll injection; UserPromptSubmit `additionalContext`) are inert, and the fallback (`hcom term inject`) has no port because hcom didn't spawn the pty. Practical symptom: the agent goes stale seconds after each turn and `hcom send` refuses it; a message sent mid-turn is never surfaced (model, asked directly: "None arrived during this turn. I received no hcom or bus messages to quote.").

**Identity hazards** (bugs waiting for any naive integration):
- hcom identity appears **directory-keyed**: a later grok session in the same cwd silently claimed the pre-existing identity (`bigboss`, then `niru`) instead of registering fresh.
- A grok **subagent's** `session_end` marked the parent's bus instance stopped.

### 6. Gap list (herder touchpoints)

File:line references from the current tree; classification per item. Overall shape: the trivial arms are genuinely trivial, but they are all **downstream of one blocked-upstream item (hcom)** and one **needs-design item (delivery/bootstrap seam)**.

| # | Touchpoint | What's needed | Class |
|---|---|---|---|
| 0 | **hcom (upstream binary)** | No `hcom grok` launcher, no `hcom hooks add grok`, no grok row in its tool table; delivery model (hook-stdout injection + blocking poll) is structurally incompatible with grok. Needs either grok gaining an injection seam (upstream xAI) or hcom growing a **pty-paste delivery mode** (it already wraps ptys and has `term inject` plumbing) + a grok launcher so the inject port exists. Also: parse grok's camelCase payloads properly, capture `transcriptPath` post-`session_start`, and label `tool: grok`. | **blocked-upstream / needs-design** |
| 1 | `tools/herder/internal/launchcmd/launch.go:19` `IsHcomCapable` | add `"grok"` arm | trivial-switch-arm (inert until #0) |
| 2 | `launchcmd/launch.go:31` `PinConfigDir` | `case "grok": setEnvDefault("GROK_HOME", ~/.grok)` — env var confirmed to exist | trivial-switch-arm |
| 3 | `launchcmd/launch.go:180` codex-style bootstrap threading | grok needs the **codex pattern, not the claude pattern**: SessionStart-`additionalContext` rewrite can't work (stdout discarded). Add a `GrokBootstrapBlock` delivered via **`--rules <RULES>`** (appends to system prompt — cleanest) or the initial `[PROMPT]` arg. | needs-design (small, precedent exists) |
| 4 | `hookcmd/template.go` / `hook.go:104` sessionstart intercept | Nothing to intercept for grok (no rewrite seam). Add grok analogues of `CodexBootstrapBlock` + `CodexResumeAddendum`. | needs-design (with #3) |
| 5 | `spawncmd/spawn.go:1455` `defaultPermFlag` | `case "grok": return "--always-approve"` (or `--permission-mode bypassPermissions`) | trivial-switch-arm |
| 6 | `spawncmd/spawn.go:1466` `hasExplicitPermFlag` | recognize `--permission-mode`, `--always-approve` | trivial-switch-arm |
| 7 | `lifecyclecmd/lifecycle.go:983` `permissionArgs` | same as #5 | trivial-switch-arm |
| 8 | `lifecyclecmd/lifecycle.go:356` `detectSelfAgent` | grok exports `GROK_AGENT=1` to shell children (verified) | trivial-switch-arm |
| 9 | `lifecyclecmd/lifecycle.go:241` `forkSelfFallback` | `case "grok": ["--resume", sid, "--fork-session"]` — **verified working** (fork recalled parent context under a new uuid); resume = `--resume <sid>` / `-c`, both verified | trivial-switch-arm |
| 10 | `spawncmd/spawn.go:1148` `awaitBind` + `sidecarcmd/sidecar.go:302` correlation | Unknown whether an hcom-launched grok row would carry `launch_context.pane_id` (claude-like) or need `process_id` enrichment (codex-like) — undeterminable until #0 exists | needs-design (pending #0) |
| 11 | `spawncmd/bootpaste.go:73` sigil switch | `case "grok": sigil = "❯"` (same as claude) — only needed for a **bus-less grok** spawn, which is the one mode herder could ship **today** (raw agent + boot-paste, no hcom) | trivial-switch-arm |
| 12 | Observer/transcript (`observercmd/observer.go:566`, hcom transcript) | grok transcript = `chat_history.jsonl` in a grok-specific layout; hooks advertise `updates.jsonl` instead, and not at session_start. A grok transcript parser / path mapping is new work. | needs-design |

**If findings call for code, that's a separate implement task** (per this unit's charter), with this report as reference.

### Safety-rail compliance (AC #6)

- All grok runs in `$SCRATCHPAD/grokchar/` (git-init'd scratch project); private tmux server `-L grokchar`, killed at the end.
- All hcom traffic from experiments went to the isolated `HCOM_DIR=$SCRATCHPAD/grokchar/.hcom` bus (`bigboss`/`lara`/`niru` live only there). The live bus was checked (read-only) immediately after the one pre-isolation headless sanity run: no registration appeared on it.
- No herder/hcom code or config touched. One side effect outside scratch: `/hooks-trust` created `~/.grok/trusted_folders.toml` (file did not previously exist) — **removed after the experiments**; grok also wrote its normal session dirs under `~/.grok/sessions/` (inert data, left in place).
- API key: pre-existing `XAI_API_KEY` used; nothing was signed up for.
