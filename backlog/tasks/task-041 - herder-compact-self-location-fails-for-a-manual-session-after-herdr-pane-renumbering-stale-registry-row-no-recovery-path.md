---
id: TASK-041
title: >-
  herder compact: self-location fails for a manual session after herdr pane
  renumbering (stale registry row, no recovery path)
status: To Do
assignee: []
created_date: '2026-07-08 04:34'
updated_date: '2026-07-16 03:35'
labels: []
dependencies: []
priority: medium
ordinal: 41000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
herder compact self-location refuses for manual/enrolled sessions whose registry coordinates or herdr detection state have drifted. Three live hits + one field report (see comments for full evidence); CURRENT CONSOLIDATED SCOPE (supersedes the original description):

(a) PANE-LIST FALLBACK: compact self-location checks the herdr AGENT list only; a detection-lost-but-alive caller (pane alive and readable, agent absent from agent list — the herdr-upgrade breakage class, and shell-relaunched sessions per TASK-070) is refused even with correct registry coordinates. Give compact the same tri-state treatment TASK-046 gave wait/list: fall back to the pane list + guid/label match when the agent list has no entry.

(b) RECOVERY AFFORDANCE: the refusal text diagnoses well (identity chain: no HERDER_GUID, no session match, no active row) but never says HOW to re-prove identity. Refusal must name concrete recovery steps (re-enroll, or the manual pane-injection workaround: herdr pane send-keys <own-pane> ctrl+u, send-text the /compact command, send-keys enter).

(c) CWD CORROBORATION TOO STRICT: compact also refuses when the invoking shell cwd is a SUBDIRECTORY of the pane foreground cwd (lale field report); accept subdirectory matches.

Fail-closed remains correct in all cases — nothing may be typed into an unverified pane. Related: TASK-035 fixed this disease class for send; TASK-046 for wait/list.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:04
---
vibe (herdr-0.7.3 audit, bus #5629, applied by hera): herdr 0.7.0 #569: pane ids are stable handles and closed ids no longer retarget — the renumbering trigger for the original failure likely cannot recur in-session (re-verify; server handoff/restart still reissues coordinates per TASK-046, so the stale-registry-row state remains reachable). Per hera: the recovery-affordance half stands regardless — the refusal message must say HOW to re-prove identity. Suggest re-scoping title to the affordance; TASK-034's blocker status should be re-evaluated after TASK-050 (NEW-4) re-verification.
---

created: 2026-07-08 06:45
---
SECOND live hit, new mechanism (hera, 2026-07-08, post-046): herder compact refused with correct-coordinates row — 'terminal term_65612408bc9034 not live in herdr agent list' — because compact self-location checks the AGENT LIST, and heras pre-handoff process is detection-lost (herdr-upgrade breakage class 2) while the PANE is alive and readable (wait --read fine). Fail-closed still correct, but the liveness source is wrong: compact needs the TASK-046 tri-state treatment — pane-list fallback + guid/label match — or at minimum the detection-lost guidance wait got. Re-scope this ticket to: (a) pane-list fallback in compact self-location, (b) recovery-affordance refusal text. Workaround used: direct herdr pane send-keys injection into own verified pane.
---

created: 2026-07-08 09:48
---
[hera 2026-07-08] THIRD live hit at owner-called compact: refusal text is now the improved self-identity chain ('no HERDER_GUID, no session match, no active row for terminal term_65612408bc9034... Nothing was typed') — better diagnosis than hit 2, still no recovery affordance and still no pane-list fallback for a detection-lost-but-alive caller pane. Workaround (ctrl+u + send-text + enter into own pane) used again, worked again. Scope unchanged.
---

created: 2026-07-08 11:31
---
lale field data (#11888), second refusal mode (benign): herder compact also refuses when the invoking shell cwd is a SUBDIRECTORY of the pane foreground cwd (cwd corroboration too strict); running from the repo root cleared it. Distinct from the own-pane refusal already on this task; fold both into whatever loosens compact's corroboration.
---
<!-- COMMENTS:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 compact from a detection-lost-but-alive pane (agent list empty for the pane, pane list + registry coordinates agree) succeeds via the pane-list fallback
- [ ] #2 every self-location refusal message names at least one concrete recovery step; no refusal ends at diagnosis only
- [ ] #3 compact invoked from a subdirectory of the pane foreground cwd is accepted
- [ ] #4 contract suite covers the fallback path, the refusal wording, and the subdirectory case
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-07-13 staleness audit (read-only, evidence-verified): AMEND not close. compact --then shipped but self-location remains: paste target resolved only from herdr agent list with refusal when absent (spawncmd/compact.go:146-164, hera spot-verified), wd must equal paneCWD exactly (287-295), early self-row refusal lacks concrete recovery (91-94). Pane re-key/durable-key correlation DID improve (69-83, 250-280; f210777). Remaining scope: pane-list fallback + recovery wording + subdir corroboration.

FRESH LIVE EVIDENCE 2026-07-16: a peer orchestrator's manual session hit the exact class post-0.7.4-handoff — seat re-enrolled fine (guid/pane/terminal recorded) but the terminal is absent from herdr agent list (detection-lost), so herder compact refuses 'cannot locate your own pane'. Recovery today = owner types /compact directly. Any fix should consider the detection-lost case (registry row healthy, agent-list absent), not only pane renumbering.

RESUME-PATH REFINEMENT (peer datapoint, 2026-07-16): a full process restart via session RESUME does NOT heal detection-lost — the session re-enters its existing pane instead of launching through the shim, so launch-time detection never fires; the terminal stays absent from the agent list while sibling sessions are detected. herder enroll succeeds (registry healthy) but compact still refuses on self-location. Practical consequence: sessions resumed in place CANNOT self-compact; the human types /compact. Correction to prior operational advice: restart heals detection only when the relaunch goes through the shim/launch path (fresh pane) — resume-in-place does not. The fix must cover the resume path, not just fresh launches.

STALE-ENV VARIANT (peer datapoint, 2026-07-16): an owner-restarted session booted with a HERDR_PANE_ID in env pointing at an UNRESOLVABLE pane (herdr pane get fails on it; the session's real pane differs) — so every herder verb needs an explicit HERDR_PANE_ID override or the enroll leg dies. Same family: restarted/resumed-in-place sessions inherit stale or foreign pane ids from the prior environment. Working end-to-end recovery on record: herder adopt <old-guid> --confirm-dead with the pane override (new guid seated, label transferred, old seat retired, bus identity reclaimed). Fix scope addition: launch-time pane-id env must be VALIDATED against a resolvable pane (and refreshed or dropped when stale), not trusted; the adopt recovery should be named in the refusal text.
<!-- SECTION:NOTES:END -->
