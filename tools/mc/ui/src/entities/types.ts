// Wire types for /api/v1 — the frontend expression of the Go DTOs in
// tools/mc/api.go, one property per DTO field, same names. The raw JSON
// keys are pinned server-side (TestAPIWireShapeRawKeys, api_test.go); a
// change on either side without the other is a contract break.

// Every source-backed payload/section carries its provenance stamp: the
// system of record, the observation time, and an opaque content-derived
// version token. Provenance IS the invalidation contract (ARCHITECTURE.md
// §"The wire contract").
export interface Provenance {
  source: string;
  observedAt: string;
  version: string;
}

// GET /api/v1/version — the pollable invalidation contract, scope-aware.
// A missions-list client polls bare and watches `missions`; a
// mission-detail client polls ?mission=<slug> and watches `mission` +
// `journal` + `roster` (the stamps of its three sections). `mission` is
// present only on a scoped poll.
export interface VersionStamps {
  journal: Provenance;
  missions: Provenance;
  roster: Provenance;
  mission?: Provenance;
}

export interface TaskCount {
  status: string;
  count: number;
}

export interface Mission {
  slug: string;
  ok: boolean;
  name: string;
  owner: string;
  authority: string;
  status: string;
  created: string;
  boardAvailable: boolean;
  taskTotal: number;
  taskCounts: TaskCount[];
  warnings: string[];
}

// GET /api/v1/missions
export interface MissionsPayload {
  missions: Mission[];
  warning: string;
  provenance: Provenance;
}

export interface Thread {
  id: string;
  title: string;
  status: "open" | "closed";
  grade: string;
  expects: string;
  openedBy: string;
  with: string[];
  turn: string;
  updated: string;
  messageCount: number;
  lastFrom: string;
  lastText: string;
}

export interface RosterAgent {
  name: string;
  address: string;
  tool: string;
  status: string;
  detail: string;
  unread: number;
  role: string;
  branch: string;
  missionSource: string;
  unmanaged: boolean;
}

// GET /api/v1/mission/{slug} — three per-source sections, each stamped by
// its own system of record; provenance is never response-global because
// the sections are observed at different times.
export interface MissionSection {
  status: Mission;
  provenance: Provenance;
}

export interface ThreadsSection {
  rows: Thread[];
  provenance: Provenance;
}

export interface RosterSection {
  agents: RosterAgent[];
  warning: string;
  provenance: Provenance;
}

export interface MissionDetailPayload {
  mission: MissionSection;
  threads: ThreadsSection;
  roster: RosterSection;
}
