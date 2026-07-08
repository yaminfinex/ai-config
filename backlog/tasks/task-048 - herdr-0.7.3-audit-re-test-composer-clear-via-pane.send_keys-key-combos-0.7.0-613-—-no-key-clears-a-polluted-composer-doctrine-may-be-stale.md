---
id: TASK-048
title: >-
  adopt ctrl+u composer-clear in recovery paths (bootpaste/spawn recovery,
  cull/send help text) — clear-key doctrine falsified live on 0.7.3
status: To Do
assignee: []
created_date: '2026-07-08 05:04'
updated_date: '2026-07-08 08:39'
labels: []
dependencies: []
priority: medium
ordinal: 48000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Filed by hera on behalf of vibe (herdr-0.7.3 upgrade audit, bus #5629) — applied verbatim per single-writer protocol.

Upstream pane.send_keys/pane.send_input.keys now accept Herdr key-combo strings (ctrl+h/j/k/l cited; parser likely general). Our doctrine (docs/spawn-patterns.md:155) says only Enter/esc/C-c are accepted and BSpace/C-u are rejected invalid_key, hence 'just submit through pollution'. Re-test ctrl+u / ctrl+backspace on a scratch pane on 0.7.3; if a clear key exists, update spawn-patterns doctrine and give bootpaste/send a real polluted-composer recovery path instead of submit-through (codex double-submit tolerance no longer load-bearing). Small, self-contained, no blockers.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:08
---
vibe (herdr-0.7.3 audit, bus #5689, applied by hera): Verification half DONE — doctrine FALSIFIED live on 0.7.3: a composer-clear key exists and works end-to-end. Evidence (2026-07-08, disposable probes keyprobe-b4a557db/clprobe-e7ebebb4/cxprobe-4e5b86b5, all culled): (1) `herdr pane send-keys` accepts herdr-native combo strings — ctrl+u and backspace accepted+delivered; tmux-style C-u/BSpace still rejected invalid_key (capability is new; docs must not suggest tmux syntax). (2) Claude composer: ctrl+u clears cleanly (native kill-line, 'Ctrl+Y to paste deleted text' shown). (3) Starvation re-confirmed on 0.7.3: queued hcom message did NOT inject while composer polluted. (4) Clear->unblock proven: after ctrl+u the starved message injected immediately and the probe replied on the bus (#5661 UNBLOCKED). (5) Codex composer: ctrl+u clears too (placeholder restored). New doctrine for spawn-patterns.md:155: polluted composer -> `herdr pane send-keys <pane> ctrl+u`; queued bus delivery resumes at next boundary. Submit-through and codex double-submit tolerance no longer load-bearing. BOARD DECISION (hera): task re-scoped to the remaining CODE half — adopt ctrl+u in bootpaste/spawn recovery + cull/send help text; the doc rewrite folds into TASK-049's sweep.
---

created: 2026-07-08 08:18
---
[hera 2026-07-08] Vibe hand-back (#9373): worker task048-nezu, 1 commit 74fd3e0, vibe gate independently green. HERA GATE GREEN from worktree: vet/test both modules (spawncmd -count=1 fresh), 22/22 suites. Fence held (no internal/registry). Opus adversarial review dispatched: review-048-solo (guid 33b771d5, own tab), brief napkins/run-herder-dx/brief-review-048.md. Reviewer explicitly rules on: (1) the endorsed deviation — bash "$" composer sigil (recovery was inert with sigil ""; risk = scrollback dollar-space false-positive -> loud code-2 refusal on previously-working bash+--prompt; tighten with line anchoring vs accept); (2) queued-bus-message-vs-garbage — code path AND docs wording (cleared queued message re-injection is UNVERIFIED); plus ctrl+u wrong-state/double-fire, fail-closed integrity (TASK-024 floor byte-untouched through the pasteResult refactor), golden reality (fixtures must exercise the re-read). MEDIUM+ blocks merge.
---

created: 2026-07-08 08:25
---
[hera 2026-07-08] Opus adversarial verdict (review-048-solo, #9466): no BLOCKER; 1 MEDIUM, 2 LOW, 1 NIT. Angle rulings: bash "$" sigil ACCEPTED (bottom-up last-sigil anchor symmetric with composerConfirmedEmpty; worst case fails closed, loud refusal never mis-type); queued-message angle TIGHTEN. MEDIUM: the queued/no-receipt hint (spawn.go:1304 + 4 doc mirrors + bus_queued golden) recommends ctrl+u on the input line in exactly the branch where a queued message renders there — operator following it destroys an in-flight delivery; old Enter-hint was harmless, new hint is destructive, no caveat. LOW(code): compact.go:170 auto-recovery on the caller's own pane can ctrl+u a rendered queued message if the compacting agent is composer-starved; whether hcom re-injects is UNVERIFIED -> possible silent loss. LOW(accepted): bash false-positive fails closed. NIT: polluted_still refusal reuses the modal message + circular clear-and-retry. Probed clean: exactly-one-ctrl+u (single if, golden-pinned), ownership assumptions, fail-closed integrity + TASK-024 chain byte-untouched, fixtures genuinely exercise the re-read (mock flips composer_cleared state between reads), doc tmux-syntax sweep. DISPOSITION: MEDIUM+NIT fix round to nezu via vibe; compact LOW settled EMPIRICALLY (vibe live-tests ctrl+u on a queued message with disposables — re-inject vs lost — result recorded here; guard/caveat scoped if lost). Reviewer static-only (no go1.26) — execution covered by hera gate pre-review.
---

created: 2026-07-08 08:39
---
[hera 2026-07-08] Round 2 (#9703): 4d8dec3 fixes MEDIUM at all 5 sites (do-not-clear caveat for queued messages) + NIT properly (typed Refusal cause blocked|composer_polluted; non-circular guidance). Hera regate green on branch tree (22/22). LIVE-VERIFY ANSWERED (vibe, disposable probe 922dcd6d): queued bus message is NOT lost to ctrl+u — marker sent mid-turn, pane cleared before delivery, hcom delivered at the boundary and probe acked. hcom holds queued messages in its own store and injects regardless of composer state; NO compact.go guard needed; solo LOW closed empirically. BONUS FINDING: queued text NEVER rendered on the composer line (claude + hcom 0.7.23, three attempts) — the "queued renders on input line" sharp-edge claim is version-stale (possibly described the retired keystroke transport); do-not-clear caveat kept as defensive wording; CODEX render behavior unverified. INTEGRATION: 048 is second lander behind A3 which touched every spawn golden — nezu to merge main in-branch, regenerate new goldens post-A3, full 23-suite gate; then hera regate + solo delta on the final tree.
---
<!-- COMMENTS:END -->
