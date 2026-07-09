# Session-Shipping Prior Art

RESEARCH MEMO — input to the Q18 ruling (boundaries doc §6b)

Date: 2026-07-09. Scope: prior art for the ratified shape — per-node dumb byte-shipper (inotify tail, per-file byte-offset cursors, no parsing) → central byte-faithful mirror → parse-on-ingest index → simple team surface, keyed by (tool, session_id), Tailscale-network auth only. Web-verified; where the ecosystem moved past Jan 2026 the search results were trusted over memory. This memo does not evaluate the mission/herder design.

---

## Q1 — Existing tools: does anything already do the whole job?

**VERDICT: No. Nothing found does "byte-ship raw harness session files from many machines to one central team mirror." The ecosystem is (a) local single-machine viewers, (b) single-user multi-device sync via dumb cloud storage, (c) parsed-event relay products, (d) OTel metric dashboards. The closest analogs validate the pieces but nobody has assembled this exact thing. Build is justified.**

Survey, by what each ships:

| Tool | What it is | What it ships |
|---|---|---|
| [claude-code-log](https://github.com/daaain/claude-code-log) (daaain) | Python CLI, JSONL → HTML/Markdown | Nothing off-machine; local render |
| [sniffly](https://github.com/chiphuyen/sniffly) (Chip Huyen) | Local analytics dashboard (usage stats, error analysis) at localhost:8081 | Local parse of ~/.claude; optional **share link of parsed stats**, not raw logs |
| [simonw/claude-code-transcripts](https://github.com/simonw/claude-code-transcripts) | Publish sessions as multi-page HTML | Rendered HTML (gist/pages), one-shot, not a live mirror |
| [claude-code-trace](https://github.com/delexw/claude-code-trace), [claude-JSONL-browser](https://github.com/withLinda/claude-JSONL-browser), [claude-session-dashboard](https://github.com/dlupiak/claude-session-dashboard), "Agent Sessions" (macOS, multi-harness incl. Codex) | Local session browsers/TUIs | Nothing; single machine |
| [claude-sync](https://github.com/tawanorg/claude-sync) (tawanorg) | **Closest file-shipping analog.** Syncs raw ~/.claude (full or sessions-only scope) to R2/S3/GCS/WebDAV, gzip + age-encrypted, passphrase-derived keys | **Raw files**, whole-file push/pull ("git for your Claude conversations"). Single-user multi-device; no central server, no viewer, no team surface, no tailing/incremental offsets |
| [Omnara](https://github.com/omnara-ai/omnara) (YC S25) | "Command center for your coding agents" — web+mobile dashboard, fleet view, human-in-the-loop approvals; Claude Code + Codex | **Parsed events over the wire** via wrapper/SDK/REST relay to their (self-hostable) server. Real-time relay, not a byte mirror; per-user command center, not a team transcript archive |
| [cot.run](https://cot.run/) | Self-hosted observability for Claude Code / Cursor / Codex — session tracing, timelines, search, usage metrics | Parsed traces, described as local/self-hosted; no evidence of multi-node team mirror (site is thin) |
| [Anthropic's own team analytics](https://code.claude.com/docs/en/analytics) (Team/Enterprise) | Usage dashboard: spend, tokens, lines accepted per user | **Metrics only**, no transcripts |
| Claude Code [Remote Control](https://code.claude.com/docs/en/remote-control) | Continue a local session from phone/browser | Live relay of one session; not an archive, not team-wide |
| OTel dashboard stacks ([SigNoz](https://signoz.io/docs/claude-code-monitoring/), [OpenObserve](https://medium.com/devops-ai/openobserve-claude-code-end-to-end-ai-observability-984afcaeba36), [Grafana Codex integration](https://grafana.com/docs/grafana-cloud/monitor-infrastructure/integrations/integration-reference/integration-openai-codex/), [KB1SLN-Labs/agent-observability](https://github.com/KB1SLN-Labs/agent-observability)) | Central collectors + dashboards | OTel metrics/events (see Q2) |

Notably: cross-device session availability is a widely-felt gap — [anthropics/claude-code#47926](https://github.com/anthropics/claude-code/issues/47926) requests native cross-device resume; Anthropic says it's on the roadmap. People solve it today with Syncthing/NAS symlinks/iCloud ([steeman.be](https://www.steeman.be/posts/syncing-claude-code-across-multiple-machines/), [claude-session-sync](https://github.com/Dinesh3184/claude-session-sync)) — i.e. dumb file replication of the raw JSONL, which is exactly our shipper minus the index. No one found layers a team surface on top.

## Q2 — Could OTel replace file shipping?

**VERDICT: No for this product, but the gap is narrower than folk memory says. OTel is forward-only, opt-in-per-flag, per-request-shaped, and two divergent configs; the files are the only byte-faithful, backfillable, tool-uniform record. Keep file shipping; treat OTel as optional gravy.**

What changed post-cutoff (verified against [code.claude.com/docs/en/monitoring-usage](https://code.claude.com/docs/en/monitoring-usage)): Claude Code OTel is no longer metrics-plus-thin-events. With flags it can now carry real content:

- `OTEL_LOG_USER_PROMPTS=1`, `OTEL_LOG_ASSISTANT_RESPONSES=1` — prompt/response text (redacted by default)
- `OTEL_LOG_TOOL_DETAILS=1`, `OTEL_LOG_TOOL_CONTENT=1` — tool params and tool I/O bodies (60 KB truncation; needs `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` tracing)
- `OTEL_LOG_RAW_API_BODIES=1|file:<dir>` — full Messages API request/response JSON incl. conversation history

Why it still doesn't replace files for team transcript visibility:

1. **No backfill.** OTel is live emission only; the ~30 days of existing sessions on each box, and anything written while the collector is down or misconfigured, never arrive. Files can be re-shipped from offset 0 at any time.
2. **Shape mismatch.** `api_request_body` re-sends the whole conversation history per turn (massive duplication) and is keyed to API requests, not to the session artifact users resume. Reconstructing "the session as ~/.claude sees it" from OTel is a parsing project strictly harder than parsing the JSONL.
3. **Truncation and beta flags.** 60 KB truncation inline; full bodies require `file:<dir>` — which is… files on disk you'd then have to ship. The content-bearing parts sit behind an enhanced-telemetry beta flag; churn risk equals or exceeds JSONL churn.
4. **Codex divergence.** Codex CLI OTel ([config reference](https://developers.openai.com/codex/config-reference), [SigNoz guide](https://signoz.io/docs/codex-monitoring/)) is a separate `[otel]` config.toml scheme (otlp-http/grpc, `log_user_prompt`), events not transcripts, with real holes: [`codex exec` emits no OTel metrics; `codex mcp-server` emits none at all](https://github.com/openai/codex/issues/12913). Two per-tool OTel integrations vs one byte-shipper.
5. **What OTel users actually get** (per [General Analysis](https://generalanalysis.com/guides/claude-code-control-observability-opentelemetry), [SigNoz](https://signoz.io/blog/claude-code-monitoring-with-opentelemetry/), [AWS](https://aws.amazon.com/blogs/mt/analyzing-claude-code-usage-with-cloudwatch-and-opentelemetry/)): cost/token/session metrics, tool-decision events, compliance-style event trails. What they conspicuously don't get: a browsable transcript. Every "see the conversation" tool in Q1 reads the files instead.

OTel is the right answer if the product were cost/usage dashboards. It is the wrong primary transport for "what has everyone been working on."

## Q3 — Log-shipping prior art: what filebeat/fluent-bit/vector teach

**VERDICT: The dumb-shipper/central-parse split is the settled industry shape (that's the entire beats/fluent-bit/vector edge-agent thesis: parse centrally, keep the edge dumb). The hard part is file identity + cursor lifecycle, and 10+ years of their bug trackers say exactly which mechanisms to copy.**

Mechanisms worth copying:

- **Content fingerprint as file identity, not (device, inode).** Filebeat 9.0 made `file_identity: fingerprint` (hash of first 1024 bytes by default) the default ([docs](https://www.elastic.co/docs/reference/beats/filebeat/file-identity), [elastic/beats#40197](https://github.com/elastic/beats/issues/40197)) precisely because inode reuse makes the shipper resume mid-file in a brand-new file and silently skip data ([documented inode-reuse issue](https://www.elastic.co/guide/en/beats/filebeat/current/inode-reuse-issue.html), [#1341](https://github.com/elastic/beats/issues/1341)); device IDs are also unstable under LVM. Vector independently converged on the same design: CRC of the first N lines as the checkpoint key ([Vector file source](https://vector.dev/docs/reference/configuration/sources/file/), [checkpointing highlight](https://vector.dev/highlights/2021-01-31-file-source-checkpointing/)). For us there's a cheaper primary key — session files are named by session UUID and Claude/Codex embed session ids in content — but fingerprint-style identity is the fallback for "same name, recreated file."
  - Copy the known edge too: fingerprinting can't identify files shorter than the fingerprint window — Filebeat wouldn't ingest files <1 kB ([#44780](https://github.com/elastic/beats/issues/44780)). Freshly-created session files are tiny; identity must work at size ~0 (name/UUID first, fingerprint once big enough).
- **Persistent cursor registry, updated only after durable handoff.** Filebeat registry / Vector `data_dir` checkpoints / fluent-bit SQLite `db` for tail offsets ([fluent-bit tail docs](https://docs.fluentbit.io/manual/data-pipeline/inputs/tail)). All three flush cursor state after the downstream ACK → **at-least-once, never at-most-once**. Consequence: the receiver must be idempotent. Central mirror keyed by (tool, session_id, byte-offset) makes resend trivially idempotent — duplicate byte ranges are compared/overwritten, not appended. This is the strongest argument *for* the byte mirror: idempotency falls out of addressing bytes, whereas event-level dedup needs event ids.
- **Cursor GC.** Filebeat's `clean_inactive`/`clean_removed` exist because the registry otherwise grows forever and, worse, stale entries + inode reuse = wrong resume. Claude Code deletes transcripts after 30 days (`cleanupPeriodDays`) so deletion is routine, not exceptional: shipper must drop cursors on delete and must not confuse delete+recreate with truncation.
- **Truncation detection = size < cursor.** And it's genuinely fiddly: Filebeat had a bug where truncation was detected but the registry offset not reset, causing infinite re-ingest ([elastic/beats#38070](https://github.com/elastic/beats/issues/38070)). copytruncate rotation loses lines written between copy and truncate ([rotation pitfalls doc](https://www.elastic.co/guide/en/beats/filebeat/current/file-log-rotation.html)) — irrelevant for us (no logrotate on session files) but the lesson stands: on size regression, reset to 0 and re-ship; let the idempotent mirror absorb it.
- **inotify is a hint, not a guarantee.** fluent-bit reconciles all watched files by rescan when the inotify queue overflows ([tail plugin internals](https://deepwiki.com/fluent/fluent-bit/5.1.1-tail-input-plugin)); Vector/Filebeat poll-scan regardless. Ship on inotify, but run a periodic full scan to catch missed events, renames (Claude `/cd` moves session files between project dirs — see Q4), and files created while the shipper was down.
- **Backpressure = stop reading, not buffer unbounded.** All three block the tail when the sink is down and resume from the cursor; the file itself is the buffer. For session files this is free — the data already lives on disk with ~30 days retention; the shipper needs no queue at all beyond its cursor file.

## Q4 — Anything that changes our shape?

**VERDICT: No prior art argues for a parsing shipper for this workload — the opposite: Anthropic explicitly documents the JSONL entry format as internal and version-unstable, which is the argument for keeping parsing central (one deploy) and the wire byte-faithful. But "append-only" is only mostly true: verified churn (resume writing new files, duplicate-history bugs, `/cd` relocation, interleaved concurrent writers, 30-day deletion) means naive per-path offset tailing is insufficient — the design's (tool, session_id) keying + parse-on-ingest dedup is not optional, it's load-bearing.**

Verified write semantics:

- **Claude Code** ([official sessions doc](https://code.claude.com/docs/en/sessions)): `~/.claude/projects/<project-dir-slug>/<session-uuid>.jsonl`, one JSON object per line. Normal operation is append-only per file — one line per message/tool-use/meta entry ([format reference](https://claude-dev.tools/docs/jsonl-format), [Yi Huang deep-dive](https://databunny.medium.com/inside-claude-code-the-session-file-format-and-how-to-inspect-it-b9998e66d56b)); `/compact` appends a boundary+summary rather than rewriting. **But**:
  - The docs state outright: "The entry format is internal to Claude Code and changes between versions, so scripts that parse these files directly can break on any release." That is the parse-on-ingest argument in Anthropic's own words — a parsing shipper on N nodes breaks on every release; a central parser breaks in one place.
  - Resume has churned and misbehaved: `claude -p --resume <id>` has created a *new* JSONL file streaming only new messages; stream-json resume *duplicated the entire prior history into the file per message* ([#5034](https://github.com/anthropics/claude-code/issues/5034)); resume writes `file-history-snapshot` entries whose ids collide with message uuids and break parentUuid chains ([#36583](https://github.com/anthropics/claude-code/issues/36583)). One logical session can span multiple files with overlapping content → key by (tool, session_id), dedupe by message uuid at ingest.
  - `/cd` **relocates the session file to another project directory** (v2.1.169+), and resuming the same session in two terminals **interleaves both into one transcript** (documented). Renames/moves and mid-file surprises are normal, not edge cases.
  - `cleanupPeriodDays` defaults to 30-day deletion — the central mirror will outlive the source files; deletion handling is mandatory (and longer retention is a genuine product win of the mirror).
  - Byte-tail caveat: appends are line-buffered writes, but a reader can see a partial last line; the shipper can ship partial bytes safely (byte mirror doesn't care) while the ingest parser must hold back the trailing incomplete line.
- **Codex CLI** ([session/rollout discussion #3827](https://github.com/openai/codex/discussions/3827), [features doc](https://developers.openai.com/codex/cli/features)): `~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<uuid>.jsonl`; line 1 is a session-meta header, subsequent lines are events; resume **appends to the same rollout file**, session id stable. Cleaner than Claude's, but same conclusion: upstream-owned, versioned, churns (archiving/lifecycle features landed v0.136+).
- **Parsing-shipper prior art:** Omnara (wrapper relaying parsed events) and the OTel route (Q2) are the structured-events-on-the-wire designs. Both accept the cost that when the upstream format shifts, every edge must update, and neither yields a replayable byte-faithful record. Found nothing arguing against byte mirrors; log-shipping literature (Q3) treats "raw at the edge, parse centrally" as the default posture for exactly this kind of format churn.

Net shape adjustments the evidence forces (all compatible with the ratified design): per-file cursors must survive rename/move (identity ≠ path), size-regression → reset-and-reship, deletion ≠ truncation, ingest dedup by message uuid across files of one session, tolerate trailing partial lines.

## Q5 — Tailnet-identity-only auth

**VERDICT: Defensible for v1 for a small team's internal transcript service, with two required hygiene items: (a) scope access with an ACL/grant rather than "whole tailnet" — tailnets contain phones, CI boxes, and shared nodes; (b) if identity headers are used, only trust them when injected by tailscaled/tsnet, never accepted from the client. This is Tailscale's own advertised pattern, not an improvisation.**

- Tailscale's position is that identity accompanies every packet, cryptographically bound via WireGuard and sourced from the IdP ([Tailscale identity](https://tailscale.com/docs/concepts/tailscale-identity), ["zero trust with zero clicks"](https://tailscale.com/blog/zero-trust-with-zero-clicks-a-new-take-on-idps)). Their first-party patterns for exactly this use case:
  - **`tsnet`**: embed the node in the service; `WhoIs()` on each connection gives the authenticated user/device, and grants deliver app capabilities ([app capabilities blog](https://tailscale.com/blog/app-capabilities), [grants docs](https://tailscale.com/kb/1537/grants-app-capabilities)). Best fit for our ingest+web surface: no cert/token machinery at all.
  - **Serve identity headers** (`Tailscale-User-Login` etc.): fine when the app sits behind `tailscale serve` on the same node; the trust boundary is that tailscaled sets/strips them — do not honor them from any other hop.
  - **Grants** (GA, [announcement](https://tailscale.com/blog/grants-ga)) give per-user/per-group access to the service in the tailnet policy file — the cheap fix for "everyone on the tailnet including the NAS can read all transcripts."
  - **tsidp** ([repo](https://github.com/tailscale/tsidp), [blog](https://tailscale.com/blog/building-tsidp)) is the documented escape hatch when a real OIDC login is wanted later — an upgrade path that doesn't invalidate the v1 posture.
- Caveats that keep it "v1" rather than "done": transcript content is high-sensitivity (prompts, code, secrets pasted into sessions); tailnet identity authenticates the *device/user*, not intent — a compromised laptop on the tailnet reads everything; there is no per-session ACL story. For a small trusted team this matches how they already treat each other's shells, which is the honest threat model. Add audit logging at ingest/read and revisit if the team or content sensitivity grows.

---

## Bottom line

Build-vs-adopt: **build the shipper/mirror; adopt the mechanisms.** No existing tool does the job (Q1); the two near misses are claude-sync (right transport idea, wrong topology: no center, no team, whole-file not incremental) and Omnara (right team surface, wrong transport: parsed relay, no archive, product dependency). OTel cannot substitute (Q2). The byte-shipper design is the industry-standard split and its failure modes are fully mapped by filebeat/vector/fluent-bit — copy fingerprint identity, ACK-then-advance cursors, rescan-behind-inotify, reset-on-truncate, cursor GC (Q3). The one place the evidence pushes on the design is Q4: Claude Code's "append-only" has documented exceptions (multi-file sessions, duplicated history, file moves, interleaved writers, 30-day deletion), so (tool, session_id) keying with message-uuid dedup at ingest must be treated as core correctness machinery, not polish. Tailnet-only auth is a defensible v1 if scoped by a grant and headers are only trusted from tailscaled (Q5).
