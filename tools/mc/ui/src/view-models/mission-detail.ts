import type { MissionDetailPayload, RosterAgent, Thread } from "@/entities/types";
import { missionTaskSummary, missionTitle } from "@/view-models/mission-list";

// The thread sort law and preview derivation live here — a component
// containing a sort order or a derivation fails review (ARCHITECTURE.md).

export const expectsRank: Record<string, number> = { decide: 0, act: 1, reply: 2, read: 3 };

export const PREVIEW_LIMIT = 280;

export interface ThreadPreviewVM {
  from: string;
  text: string;
}

export interface ThreadRowVM {
  id: string;
  title: string;
  expects: string;
  status: "open" | "closed";
  /** Derived facts, rendered in the fact grain (mono). */
  facts: string;
  preview: ThreadPreviewVM | null;
}

export interface AgentRowVM {
  name: string;
  facts: string;
}

export interface MissionDetailVM {
  slug: string;
  title: string;
  /** Derived facts of the mission section (owner · status · created), fact
   * grain; null when the manifest carries none — render nothing, not a blank. */
  facts: string | null;
  /** null = board unavailable: gap honesty, render nothing rather than fake. */
  taskSummary: string | null;
  warnings: string[];
  threads: ThreadRowVM[];
  agents: AgentRowVM[];
  rosterWarning: string | null;
}

/**
 * The thread sort law: open before closed, then expectation rank
 * (decide · act · reply · read), then latest activity first.
 */
export function sortThreads(threads: Thread[]): Thread[] {
  return [...threads].sort((a, b) => {
    if ((a.status === "open") !== (b.status === "open")) {
      return a.status === "open" ? -1 : 1;
    }
    const rankA = expectsRank[a.expects] ?? expectsRank.read ?? 3;
    const rankB = expectsRank[b.expects] ?? expectsRank.read ?? 3;
    if (rankA !== rankB) {
      return rankA - rankB;
    }
    return b.updated.localeCompare(a.updated); // RFC3339 sorts lexically
  });
}

export function threadPreview(thread: Thread): ThreadPreviewVM | null {
  if (thread.messageCount === 0 || thread.lastText === "") {
    return null;
  }
  const text =
    thread.lastText.length > PREVIEW_LIMIT
      ? `${thread.lastText.slice(0, PREVIEW_LIMIT - 1)}…`
      : thread.lastText;
  return { from: thread.lastFrom, text };
}

export function threadRowVM(thread: Thread): ThreadRowVM {
  const facts = [
    thread.openedBy,
    `${thread.messageCount} msg`,
    thread.updated.slice(0, 16).replace("T", " "),
  ].join(" · ");
  return {
    id: thread.id,
    title: thread.title,
    expects: thread.expects,
    status: thread.status,
    facts,
    preview: threadPreview(thread),
  };
}

export function agentRowVM(agent: RosterAgent): AgentRowVM {
  const facts = [agent.role, agent.tool, agent.status, agent.branch]
    .filter((part) => part !== "")
    .join(" · ");
  return { name: agent.name, facts };
}

export function missionDetailVM(payload: MissionDetailPayload | undefined): MissionDetailVM | null {
  if (!payload) {
    return null;
  }
  const status = payload.mission.status;
  const factParts = [
    status.owner === "" ? "" : `owner ${status.owner}`,
    status.status,
    status.created,
  ].filter((part) => part !== "");
  return {
    slug: status.slug,
    title: missionTitle(status),
    facts: factParts.length > 0 ? factParts.join(" · ") : null,
    taskSummary: missionTaskSummary(status),
    warnings: [...status.warnings],
    threads: sortThreads(payload.threads.rows).map(threadRowVM),
    agents: [...payload.roster.agents].sort((a, b) => a.name.localeCompare(b.name)).map(agentRowVM),
    rosterWarning: payload.roster.warning === "" ? null : payload.roster.warning,
  };
}
