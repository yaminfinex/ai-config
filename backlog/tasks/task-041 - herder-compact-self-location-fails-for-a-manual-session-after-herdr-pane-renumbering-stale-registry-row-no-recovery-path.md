---
id: TASK-041
title: >-
  herder compact: self-location fails for a manual session after herdr pane
  renumbering (stale registry row, no recovery path)
status: Done
assignee: []
created_date: '2026-07-08 04:34'
updated_date: '2026-08-23 10:18'
labels: []
dependencies:
  - TASK-303
priority: medium
ordinal: 41000
---

## Teardown reconciliation — 2026-08-24

Closed by deletion. Herder compact retired; third-party compaction is
status-check + `hcom term inject` + one queued `hcom send`, and self-compaction
uses `tools/fleet/selfcompact.sh`. The composition suite carries the incident
041 successor contract.

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
herder compact self-location refuses for manual/enrolled sessions whose registry coordinates or herdr detection state have drifted. Three live hits + one field report (see comments for full evidence); CURRENT CONSOLIDATED SCOPE (supersedes the original description):

(a) PANE-LIST FALLBACK: compact self-location checks the herdr AGENT list only; a detection-lost-but-alive caller (pane alive and readable, agent absent from agent list — the herdr-upgrade breakage class, and shell-relaunched sessions per TASK-070) is refused even with correct registry coordinates. Give compact the same tri-state treatment TASK-046 gave wait/list: fall back to the pane list + guid/label match when the agent list has no entry.

(b) RECOVERY AFFORDANCE: the refusal text diagnoses well (identity chain: no HERDER_GUID, no session match, no active row) but never says HOW to re-prove identity. Refusal must name concrete recovery steps (re-enroll, or the manual pane-injection workaround: herdr pane send-keys <own-pane> ctrl+u, send-text the /compact command, send-keys enter).

(c) CWD CORROBORATION TOO STRICT: compact also refuses when the invoking shell cwd is a SUBDIRECTORY of the pane foreground cwd (lale field report); accept subdirectory matches.

Fail-closed remains correct in all cases — nothing may be typed into an unverified pane. Related: TASK-035 fixed this disease class for send; TASK-046 for wait/list.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 compact from a detection-lost-but-alive pane (agent list empty for the pane, pane list + registry coordinates agree) succeeds via the pane-list fallback
- [ ] #2 every self-location refusal message names at least one concrete recovery step; no refusal ends at diagnosis only
- [ ] #3 compact invoked from a subdirectory of the pane foreground cwd is accepted
- [ ] #4 contract suite covers the fallback path, the refusal wording, and the subdirectory case
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-08-21 tactical unblock (hotfix-vemo): branch task-041-compact-unblock (8ef50b7, unmerged, reza gates) pre-lands the compact slice — paste target = caller's own live pane, stored-terminal ladder + disagreement gate + credential stored-terminal check deleted, fossil mismatch = note + proceed. Deliberately narrow forerunner of slim-down charter decisions 1-2; no registry self-heal append (stays with decision-1 work). Wayfinder folded deltas into the deletion map (addendum block). Interplay with the red fixture fence: incident-041 red in tools/herder/tests/red-check-slimdown-fixtures.sh asserts exactly this slice and should flip green when the branch merges — re-run the suite at merge; 268/262 reds must stay red until the probe work. This task still closes via the fixture green + map deletions, unchanged.
<!-- SECTION:NOTES:END -->

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

created: 2026-08-23 10:18
---
Interim fix MERGED to main 2026-08-23 in ea6e1c0 (branch task-041-compact-unblock @ 28f90b2; owner-approved; field-verified on riko manual seat — the recorded refusing terminal now resolves to its own live pane). Task stays OPEN as resolved-interim: permanent resolution is by deletion at slim-down IMPL-2 (compact fossil ladder replaced by SelfProbe; readiness package spine). CORRECTION to the 2026-08-21 note below: the incident-041 red fixture does NOT flip green at this merge — it models the IMPL-2 injectable probe substrate, while the merged hotfix locates by the caller real pid ancestry; it flips at IMPL-2 (missions feb8b28). Post-merge red suite on main: 3 reds / 5 keep-greens, as amended.
---
<!-- COMMENTS:END -->
