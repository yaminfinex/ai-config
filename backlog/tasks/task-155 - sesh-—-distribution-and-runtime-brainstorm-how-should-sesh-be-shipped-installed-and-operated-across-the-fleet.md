---
id: TASK-155
title: >-
  sesh — distribution and runtime brainstorm: how should sesh be shipped,
  installed, and operated across the fleet
status: To Do
assignee: []
created_date: '2026-07-12 00:47'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 154000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: research/design (brainstorm). Deliberately NOT an implementation task: the owner wants the option space genuinely explored before any decision is made. The deliverable is an options memo with honest trade-offs; decisions stay with the owner. Recommendations are welcome but must be clearly separated from the option survey.

## Current state (starting point, not the answer)

Repo-based install aligned with the quick pattern: `just install` (go install -> GOBIN, git-stamped), `just deploy <store-url>` (renders the absolute GOBIN path into a systemd --user unit / launchd plist, store URL in a drop-in), `just restart`, `just versions` (running-image staleness via /proc). Requires the repo + Go toolchain + just on every node. The store URL is deliberately the only node-to-store coupling; the runtime refuses a stale binary against a newer cursor registry; drop-in preservation protects operator config. Single machine today; fleet rollout is the intent. All services currently stopped on the dev machine.

## Questions to explore

**Client (shipper) distribution — open options, explicitly NOT exhaustive; part of the job is finding more:**
- Status quo: repo + toolchain + just recipes on every node.
- Prebuilt artifacts via the quick site: quick.infinex.xyz already serves quick's team distribution (release recipe cross-compiles a matrix, publishes to the VM's /srv/releases with a latest pointer, teammates run `curl .../install.sh | sh`). The same channel can distribute the sesh binary the same way — no repo or toolchain on nodes.
- Self-installing binary: a `sesh` subcommand that renders/enables its own service unit and config, so a downloaded binary bootstraps a node with one command (pairs naturally with prebuilt distribution; the installer shell script logic would move into Go where it can be unit-tested).
- go install from the module path; mise-managed tool pinning; OS package managers; anything else the survey turns up.

**Backend (store / sesh serve) hosting and operations:**
- Where should the store run long-term: the current dev machine, a dedicated tailnet node, the quick-host VM alongside quickd, elsewhere? What does each mean for data gravity (the mirror grows unboundedly by design), backup/retention, and blast radius?
- How is the store deployed and upgraded (quick's deploy-server pattern is scp + sudo + systemctl restart on the VM; sesh serve currently has no deploy story at all)?
- Exposure: tailscale serve vs built-in tsnet mode vs loopback-only-behind-something; interaction with the M4 tailnet-auth milestone.
- Version skew: shipper fleet vs store during rollouts — what does the frozen wire doc already guarantee, what needs a compatibility policy?

**Fleet operations:**
- Onboarding a new node end-to-end with each option (including nodes with no repo, no toolchain, nobody logged in — the enable-linger class).
- Upgrade story: who restarts N shippers, how staleness is noticed (versions surface vs R23 refusal vs something push-based), whether partial-fleet skew is acceptable.
- Config beyond the store URL if any ever appears; keeping the URL-only coupling honest.

## Constraints that bind any option

No sudo for per-user components (owner-ruled); user-owned binary under a user-level service; store ACK durability and wire behavior are frozen (docs/specs/sesh-wire.md); R23 stale-binary refusal stays; drop-in/operator-config preservation stays; spec authority docs/specs/session-service-spec.md I1-I11.

## Deliverable

An options memo (durable doc, backlog doc alongside doc-001) mapping the space per axis (client distribution, backend hosting, deploy/upgrade, onboarding), with trade-offs, at least one non-obvious option per axis beyond those listed, what each option costs to try reversibly, and a clearly-separated recommendation section. Filed-ready follow-up task text for anything recommended. NO implementation, no machine changes, no decisions exercised — the owner decides.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Options memo durable as a backlog doc: client distribution, backend hosting, deploy/upgrade, and onboarding each surveyed with honest trade-offs, including at least one option per axis beyond those seeded in the description
- [ ] #2 The quick.infinex.xyz distribution channel is concretely assessed for sesh (what reusing the release/install.sh pattern takes, what differs for a service-carrying binary vs a plain CLI)
- [ ] #3 Backend hosting options assessed against data gravity, backup, exposure (tailscale serve vs tsnet), and the tailnet-auth milestone
- [ ] #4 Recommendations clearly separated from the survey; every recommendation carries filed-ready follow-up task text; no implementation or machine changes ride on this unit
<!-- AC:END -->
