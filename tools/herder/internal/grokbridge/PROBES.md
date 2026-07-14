# Grok transport implementation probes

Recorded against hcom 0.7.23 and the characterized Grok 0.2.93 contract. Scratch
bus/state directories were used; no live registry, bus, or model session was touched.

- P1: bare `hcom start` assigns a random four-letter identity and prints it in the
  first `[hcom:<name>]` line. `hcom start --as <name>` reclaims it. The binder stores
  that assigned name under the seat directory and always uses `--as` after restart.
- P4: transport correctness does not depend on `GROK_SESSION_ID` propagation. The
  MCP process first accepts the explicit herder-issued `HERDER_GROK_SEAT` capability;
  if a vendor child drops it, it walks parent `/proc` environments for the same
  capability. It never resolves a seat from cwd. This is testable without inference.
- P5: the prior characterization proves monitor survival during ordinary turns but
  does not qualify survival across compaction/resume. The transport's answer is to
  treat survival as unknown/may-die: EOF is tap death, messages keep queueing, reconnect
  emits one recovery count, and `list_pending` re-surfaces every unacked id. Survival
  has zero correctness weight.
- P7: no inference-free probe establishes a vendor model-context flood threshold, so
  the transport assumes no safe unbounded rate. Output is capped structurally to one
  compact payload-free line per newly queued message, no idle output, and one aggregate
  recovery line on reconnect instead of wake replay.
- P8: hcom 0.7.23 runs its one-hour inactive-row cleanup before `list` resolves an
  explicitly named identity. Anonymous bridge event polling does not refresh the
  bridge-owned row's activity clock. An identified exact-row read does refresh it;
  after forced cleanup, `start --as <durable-name>` recreates the same coordinate and
  messages queued before cleanup remain queryable. The bridge therefore refreshes the
  exact row every 15 minutes, reclaims it on absence, and drains the durable event log
  immediately after recovery. Status is healthy only after exact-row verification or
  successful recovery.
- Project MCP registration was re-characterized against the real Grok 0.2.99 binary
  under throwaway homes and working directories. A cwd-local `.grok/config.toml` with
  `[mcp_servers.hcom]`, an absolute herder command, `args = ["grok", "mcp"]`, and
  `enabled = true` appears in `grok inspect --json` as a `project` config layer and in
  `grok mcp list` as an `hcom` project server even when user `~/.grok/config.toml` is
  absent. The server-level `source.path` can incorrectly name that nonexistent user
  file, so the checked contract uses `configSources.layers` as authoritative provenance.
  Project discovery follows Grok's effective `--cwd`: moving `--cwd` away from the
  config directory resolves zero project layers. A bounded empty-home TUI launch reached
  the same project discovery path but did not reach MCP subprocess activation before its
  authentication/startup timeout. The evidence therefore proves the real vendor's
  resolved set through `inspect` and `mcp list`; it does not claim observed live-TUI tool
  activation.
