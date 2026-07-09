---
title: "Sessions + missions management — exploration corpus"
date: 2026-07-02
status: EXPLORATION — nothing settled
purpose: honest corpus of ideas/findings to seed proper brainstorm + grilling sessions
related:
  - docs/design/2026-07-02-session-management-architecture.md      # earlier snapshot → read as revealed requirements
  - docs/design/2026-07-02-missions-topology-domain-model.md       # exploration sketch (NOT a spec, see its status)
  - docs/plans/2026-07-01-002-feat-hcom-launch-substrate-plan.md   # hcom launch substrate — REAL, nearly done
---

# Exploration corpus: sessions, missions, topology

> **Read this first.** This is a corpus, not a design. One day of intensive exploration
> (2026-07-02): a critique pass over a prior architecture snapshot, a usage-grounded reframe,
> research passes (prior art, storage options), a sketched domain model, a 4-lens adversarial
> review of that sketch, and several re-thinks it triggered. **Everything here is under-explored,
> under-specified, and under-motivated beyond vibes — except §1 (ground truth) and §2 (values).**
> Nothing below §2 is a decision. The purpose of this doc is to let future sessions grill,
> brainstorm, and validate without re-deriving the day.

## 1. Ground truth (the only settled things)

**Felt pains, from real usage** (the motivating corpus — treat as facts):
- herdr agent messaging flaky (bad delivery, message overwrite); agent state introspection flaky;
  run-logs-as-comms noisy. → *being solved NOW by the hcom delivery-driver branch.*
- Terminal = great control surface, bad **observation** surface: no home for HTML artifacts, no
  files/git view, orientation loss across workspaces/panes.
- Mission-ish artifacts (plans, napkins, run state, generated HTML) live as loose files pinned to
  worktrees: pollute git status, lose provenance, disconnect from sessions, no harvest/cleanup.
- Worktree-as-workspace is the wrong boundary; "lost in workspaces."
- Machine/account isolation; multiplayer impossible. Permanent-ssh sessions = the standout win.
- Free-form task state fails — agents invent schemas; invented schemas can't be visualized.
- bottle unused (snapshot-as-entry-fee is wrong ergonomics); fork/branch would be used if instant.
- Session compaction pain — partially solved (herder-send-self compact).

**What is real code today:** herder/herdr (registry, spawn/send/list/wait/cull, notify rings,
briefs), orchestrate (playbook/run-log in gitignored napkins/, detection-gated Backlog.md unit
ledger), bottle (unused), napkins convention. **In flight & nearly done:** `feat/hcom-delivery-driver`
— driver abstraction (herdr keystroke fallback preserved), launch-through-hcom shims, team-scoped
buses (`HCOM_DIR`), Codex live-proven as first-class hcom target. This solves the top pain cluster
on its own. **Everything else in this doc: ideas only.**

**External facts verified:** Claude/Codex both persist JSONL sessions + fire SessionStart hooks
carrying session_id; plain `--resume` keeps session_id (only fork/fresh-launch mint new); tokens
ARE in JSONL (no-proxy loses only wire bytes); Cloudflare Artifacts = real git-for-agents SaaS,
closed beta; prior art: cass (index), casr (portable resume), Backlog.md, beads' retreat from
git-refs→Dolt, omnara post-mortem ("don't wrap the CLI; ride stable seams").

## 2. Values (stated by the author — judge all ideas against these)

1. **Escape hatches**: bottom is plain files + git + terminals; everything composable; degrade
   gracefully; never fight the tools.
2. **UX / problem-first**: motivated by solving felt pains, not architecture aesthetics.
3. **Incremental adoption + incremental build**: the *path* matters — validate as we go; each part
   useful alone even if the whole never lands. No big-bang.
4. **Don't rush.** hcom lands first and buys time.
5. **Single working surface**: if observation moves to web, control must be there too (a local TUI
   or local-web-with-local-commands are acceptable alternatives). Two-surface operation is the
   thing to avoid.

## 3. Idea inventory

Status legend: `grounded` (traces to a §1 pain) · `sketch` (shape only) · `explored` (one deep
pass) · `contested` (survived/changed under review) · `unreviewed` (post-review idea, never attacked).

| # | Idea | Status | Strongest support | Strongest objection / open hole |
|---|---|---|---|---|
| 1 | **Mission** as first-class boundary entity (unit of intent; N sessions/runs/worktrees/artifacts) | grounded, explored | fixes wrong-boundary + provenance pains at once | "mission" vs run vs workspace semantics thin; when is something a mission? |
| 2 | Mission home = dir outside codebases (`~/missions/<id>-<slug>/` — brief, events, backlog/, runs/, artifacts/, html/) | explored, contested | keeps work repos clean; greppable; napkins-done-right | sync/replication semantics unsettled (see #9); manifest vs events authority churned under review |
| 3 | **`mission` CLI** as THE agent-facing surface (`mission log/artifact/harvest/backlog/send/info/new/adopt/rename`) | sketch, promising | dissolves cwd/detection problems; env contract shrinks to "CLI resolves context"; agents get uniform verbs | barely explored; command semantics unspecified; late idea |
| 4 | Mission id `m-YYYYMMDD-<rand>`; dir renameable, id immutable; refs by id only | explored | fixes offline mint collisions (review: LWW data-loss) | rename mechanics (git-mv vs metadata-only) never truly settled |
| 5 | `mission adopt` = move scratch in + **leave symlink pointers** + hcom-notify members | sketch | lazy adoption matches how work actually starts; pointers keep baked paths alive | mid-run adoption vs live agents; open-handle/cross-fs edges; untested |
| 6 | Backlog.md board per mission, always scaffolded | grounded | author: free-form task state "doesn't work"; parseable → renderable | collides with existing repo-level backlog integration (probe is cwd-relative; durability story was "merges with branch") — un-reconciled |
| 7 | Run-log = thin decision journal; chatter→hcom; evidence by reference (session_id+offset) | grounded, explored | kills noisy run-log-as-comms | depends on #14 + #10 existing; offset refs break under transcript compaction/rotation |
| 8 | Node = (machine × account); **hub** = HTTP service (initially co-located); "hub talks to local node same as remote" symmetry | sketch | matches multi-account/multi-box reality; N=1 == topology | hub responsibilities kept accreting during exploration — needs decomposition |
| 9 | **Daemon-stream sync, not git-as-bus**: node daemon ships events/transcripts/artifacts over one HTTP stream (offset resume); hub materializes canonical tree; hub = SINGLE git writer (history/backup journal → GitHub) | **unreviewed** | author intuition ("sync a directory, git-as-comms feels wrong") + kills 5 review findings (rebase/sha, add -A half-writes, rename conflicts, LWW clobber, GH-down SPOF) that were all multi-writer-git artifacts | replaced git-bus AFTER the adversarial round — this shape has never been attacked; cross-node reads now hub-mediated; offline node↔node convergence lost |
| 10 | **Archive**: durable home for raw transcripts, `<node>/<harness>/<session_id>.jsonl`; v1 = local files on hub disk (author call); derived SQLite for query | grounded, contested | sessions ARE ephemeral (harness GC, dead laptops); "the legitimate half of bottle" | contract dishonesty flagged (one machine + backup cadence ≠ "durable"); daemon-down loss window; retention deferred while "archive everything"; write-through-to-object-store re-argument unfinished |
| 11 | Hub **web surface**: mission boards, artifacts/HTML, files/git state, live streams via SSE fed by daemon tail | grounded | THE observation pain; spike on another machine proved concept (janky) | UX unspecified; what exactly renders, phone experience, auth — all blank |
| 12 | **Drive from hub** by invoking existing control CLIs (herder-send/hcom send): exec locally, ssh for remote nodes; NO relay, no new protocol | grounded (value 5), sketch | author adamant: single surface; mechanism already exists as CLIs | wiring unspecified; earlier YAGNI cut of this was WRONG (mis-read the requirement) — re-cut carefully |
| 13 | **Global bus + membership join** (not per-mission team buses): message→mission attribution = registry/manifest join on sender/recipient; `mission send` = expand members | explored | per-mission buses silently degrade cross-bus sends to keystroke path (review, grounded in branch code); join needs zero hcom changes | depends on #14; broadcast semantics via CLI only sketched |
| 14 | **session_id capture into registry** — the universal join dependency | explored, UNBUILT | everything downstream (archive keys, evidence refs, mission membership, message attribution) leans on it | doesn't exist; likely a best-effort correlation race (like existing hcom_name capture); miss-behavior undefined. Candidate spike #1 |
| 15 | Fast **fork verb** (launcher-level --fork-session + new pane, no agent round trip) | grounded | author would use branching if instant; replaces bottle's live use | unspecified; separate rung |
| 16 | Harvest lifecycle: napkin → mission → durable docs; harvest = recorded act, safe-to-delete default | explored | cleanup/coherence pain | semantics thin (idempotency, provenance after history rewrites) |
| 17 | Env contract (`MISSION_ID`/`MISSION_DIR`) + `.mission` marker file for cwd-resolution | sketch | tools stay dumb; marker fixes cd-divergence + late adoption | ambient env inheritance PROVEN unreliable (hcom U3 subshell finding); subagent propagation unknown |
| 18 | Deprecate bottle (fork verb + archive replace its two halves) | grounded | author: never uses it | none — but forces #10/#15 to actually exist |

## 4. Review findings ledger (condensed — from the 4-lens adversarial round on the domain-model sketch)

Confirmed / high-confidence:
- Commit shas inside events can't survive `pull --rebase` — sha-based freshness structurally broken (3 reviewers, independently). *(Moot if #9 adopted — but #9 unreviewed.)*
- Sequential NNN ids collide across offline nodes → dir merge → silent mission loss (confidence 100).
- Per-mission team buses: cross-bus `resolve` returns Not-found → **silent fallback to brittle keystroke transport** for standing/cross-mission agents; transcripts don't partition by team anyway (config-dir pinned to real ~/.claude); membership would exist in two authorities.
- session_id capture: absent everywhere today; the join everything assumes (see #14).
- Archive labeled "durable" while mechanism = single machine + backup cadence; daemon-down = unbounded unshipped window; disk-full converts deferred-retention into the data-loss path.
- Orchestrate migration landmines: Backlog probe is cwd-relative; live runs bake literal playbook paths (adopt mid-run breaks agents); auto-push of run state bypasses the "user ships" gate + convention-not-mechanism secrets guard.
- herder-spawn is rewritten by BOTH the hcom branch (+205/−50) and any env-contract work → sequence: land hcom first.
- Transcripts are N-files-per-conversation (sidechains, subagents, compaction rotation) — any tail/ship design needs a *discovery* model, not just file-tail.
- Scope: drive-gateway cut was wrong (value 5); but hub SQLite-as-index, manifest.json-as-store, sha/webhook freshness machinery, and 3-job daemon bundling were all later-rung components leaking into v1.
- Coherence: "tool" vs daemon boundary, team=bus=HCOM_DIR drift, "standing agents" undefined, registry-vs-join-table drift.

## 5. The grilling agenda (what proper sessions must attack)

**Motivation grill** — for each inventory item: which §1 pain does it serve, what's the cheapest
thing that serves the same pain, what would we observe in usage that validates/kills it?
Kill-candidates to press hardest: hub-materialized-tree (#9), archive scope (#10), node/hub
framing itself (#8 — is "hub" one thing or three?).

**UX grill** — write the scenario pack FIRST (10–15 walkthroughs: bug-poke-becomes-mission;
phone-view-and-nudge; laptop-dies-Tuesday; adopt-mid-run; mission-end-harvest; hub-down-day;
two-accounts-one-box). Trace each through the ideas. Decisions get adjudicated by walkthrough,
not philosophy.

**Sync/topology grill** — #9 has never been reviewed: attack hub-mediated cross-node reads,
offline semantics, escape-hatch meaning when hub aggregates, single-writer-git-journal claims.
Compare seriously against: plain syncthing/rsync; per-mission git repos; hub-less local-first.

**Durability grill** — write the archive contract as an explicit promise ("line durable within N
min, survives loss of any one machine") and check mechanisms against it. RPO, retention,
daemon-liveness visibility.

**Identity grill** — spike session_id capture (candidate spike #1) before anything depends on it.
Lineage across fork/decant. The node dimension.

**Lifecycle grill** — mission establish/adopt/end semantics; mission-vs-run-vs-workspace
boundaries; what "archived" means; multi-run missions.

**Incremental-path grill** (value 3 — the author's central concern): for every candidate build unit:
useful alone? adoptable without the rest? what usage validates it before the next unit starts?
Current intuition (unvalidated): `mission` CLI + dir format is the only unit buildable now with
zero infrastructure; everything else waits.

## 6. Candidate process (a proposal, not a plan)

Constitution (frozen domain doc) → scenario pack → per-component deep specs in dependency order
(mission CLI · session_id capture · daemon · hub · surface · orchestrate migration), each
adversarially reviewed before the next → spikes for load-bearing unknowns before their spec
(the hcom U1–U5 pattern, which worked). Only next-rung components get spec'd deeply.
**Counter-position to also consider:** skip the constitution, go straight to scenario pack +
one-component build (mission CLI), let usage generate the next questions. Both honor value 3/4.

## 7. Corpus index

- `docs/design/2026-07-02-session-management-architecture.md` — original architecture snapshot
  (pre-reframe); §1/§6/prior-art still useful; architecture sections superseded by usage reframe.
- `docs/design/2026-07-02-missions-topology-domain-model.md` — the domain-model SKETCH this
  corpus distills; contains fuller mechanism text for ideas #1–#17 as they stood pre-review;
  its §9 hcom fold-in is the most reliable section (grounded in branch code).
- `feat/hcom-delivery-driver` branch — real; `skills/herder/references/delivery-drivers.md` +
  `spike-findings-hcom.md` are the ground-truth references.
- Review round (4 lenses: adversarial/scope/feasibility/coherence) — distilled in §4 above; full
  texts lived in the 2026-07-02 session and are summarized faithfully here.

**Where we are: early exploration. Lots of ideas, some really good, nothing settled.**
Next session: pick a grill from §5 (recommend: scenario pack first).
