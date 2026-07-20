---
id: TASK-296
title: >-
  launch-failure observability: surface child exit output for dead-at-birth
  panes
status: To Do
assignee: []
created_date: '2026-07-20 05:19'
labels: []
dependencies: []
ordinal: 295500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-report (second deployment): a spawn with a bad model arg killed the pane instantly with NO captured error — diagnosis only succeeded via a lucky interactive capture. Separately, bus term scrapes return blank for herdr panes mid-boot (observability gap, upstream candidate). Fix: capture and surface the child process's exit output/stderr on launch-failed panes (attach to the refusal or the seat record); consider a boot-window buffer so early death is never silent. Joins the existing silent-death/attribution class (detection near death time).
<!-- SECTION:DESCRIPTION:END -->
