// Wire types for /api/v1 — the frontend expression of the Go DTOs in
// tools/mc/api.go. One property per DTO field, same names; a change here
// without a matching server change is a contract break.

export interface VersionInfo {
  cursor: number;
  generation: number;
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

export interface MissionsPayload {
  missions: Mission[];
  warning: string;
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
  unmanaged: boolean;
}

export interface MissionDetailPayload {
  mission: Mission;
  threads: Thread[];
  agents: RosterAgent[];
  rosterWarning: string;
}
