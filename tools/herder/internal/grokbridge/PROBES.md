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
  compact payload-free line per newly queued message, at most two idle-aware nudges
  when session phase evidence is available, no idle output, and one aggregate recovery
  line on reconnect instead of wake replay. Correctness never depends on a nudge.
