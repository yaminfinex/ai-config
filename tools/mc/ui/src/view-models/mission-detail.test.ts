import { describe, expect, it } from "vitest";
import type { MissionDetailPayload, Provenance, RosterAgent, Thread } from "@/entities/types";
import {
  agentRowVM,
  missionDetailVM,
  PREVIEW_LIMIT,
  sortThreads,
  threadPreview,
  threadRowVM,
} from "@/view-models/mission-detail";

function thread(overrides: Partial<Thread> = {}): Thread {
  return {
    id: "t-1",
    title: "a thread",
    status: "open",
    grade: "task",
    expects: "reply",
    openedBy: "builder-lobo",
    with: ["human-yamen"],
    turn: "",
    updated: "2026-07-15T05:40:00Z",
    messageCount: 1,
    lastFrom: "builder-lobo",
    lastText: "which shape wins?",
    ...overrides,
  };
}

function agent(overrides: Partial<RosterAgent> = {}): RosterAgent {
  return {
    name: "builder-lobo",
    address: "@builder-lobo",
    tool: "claude",
    status: "seated",
    detail: "",
    unread: 0,
    role: "builder",
    branch: "task-x",
    missionSource: "marker",
    unmanaged: false,
    ...overrides,
  };
}

const prov: Provenance = { source: "test", observedAt: "2026-07-18T12:00:00Z", version: "v" };

function payload(
  threads: Thread[],
  agents: RosterAgent[],
  rosterWarning = "",
): MissionDetailPayload {
  return {
    mission: {
      status: {
        slug: "mission-one",
        ok: true,
        name: "Mission One",
        owner: "riley",
        authority: "hera",
        status: "active",
        created: "2026-07-15",
        boardAvailable: true,
        taskTotal: 0,
        taskCounts: [],
        warnings: [],
      },
      provenance: prov,
    },
    threads: { rows: threads, provenance: prov },
    roster: { agents, warning: rosterWarning, provenance: prov },
  };
}

describe("sortThreads — the thread sort law", () => {
  it("open before closed, then expectation rank, then latest activity first", () => {
    const rows = [
      thread({ id: "closed-decide", status: "closed", expects: "decide" }),
      thread({ id: "open-read", expects: "read", updated: "2026-07-15T01:00:00Z" }),
      thread({ id: "open-reply-old", expects: "reply", updated: "2026-07-15T02:00:00Z" }),
      thread({ id: "open-reply-new", expects: "reply", updated: "2026-07-15T09:00:00Z" }),
      thread({ id: "open-decide", expects: "decide", updated: "2026-07-15T01:00:00Z" }),
      thread({ id: "open-act", expects: "act", updated: "2026-07-15T08:00:00Z" }),
    ];
    expect(sortThreads(rows).map((row) => row.id)).toEqual([
      "open-decide",
      "open-act",
      "open-reply-new",
      "open-reply-old",
      "open-read",
      "closed-decide",
    ]);
  });

  it("ranks an unknown expectation with read, the lowest tier", () => {
    const rows = [
      thread({ id: "weird", expects: "ponder", updated: "2026-07-15T09:00:00Z" }),
      thread({ id: "read", expects: "read", updated: "2026-07-15T01:00:00Z" }),
      thread({ id: "reply", expects: "reply", updated: "2026-07-15T01:00:00Z" }),
    ];
    expect(sortThreads(rows).map((row) => row.id)).toEqual(["reply", "weird", "read"]);
  });

  it("does not mutate its input", () => {
    const rows = [thread({ id: "b", expects: "read" }), thread({ id: "a", expects: "decide" })];
    sortThreads(rows);
    expect(rows.map((row) => row.id)).toEqual(["b", "a"]);
  });
});

describe("threadPreview", () => {
  it("renders no preview for an empty thread — gap honesty", () => {
    expect(threadPreview(thread({ messageCount: 0, lastText: "" }))).toBeNull();
    expect(threadPreview(thread({ lastText: "" }))).toBeNull();
  });

  it("passes short text through untouched", () => {
    expect(threadPreview(thread())).toEqual({ from: "builder-lobo", text: "which shape wins?" });
  });

  it("truncates to the preview limit with an ellipsis", () => {
    const long = "x".repeat(PREVIEW_LIMIT + 1);
    const preview = threadPreview(thread({ lastText: long }));
    expect(preview?.text).toHaveLength(PREVIEW_LIMIT);
    expect(preview?.text.endsWith("…")).toBe(true);
  });

  it("leaves text exactly at the limit alone", () => {
    const exact = "x".repeat(PREVIEW_LIMIT);
    expect(threadPreview(thread({ lastText: exact }))?.text).toBe(exact);
  });
});

describe("threadRowVM", () => {
  it("derives the fact line in fact grain: opener · count · timestamp", () => {
    expect(threadRowVM(thread()).facts).toBe("builder-lobo · 1 msg · 2026-07-15 05:40");
  });
});

describe("agentRowVM", () => {
  it("joins only the facts that exist", () => {
    expect(agentRowVM(agent()).facts).toBe("builder · claude · seated · task-x");
    expect(agentRowVM(agent({ role: "", branch: "" })).facts).toBe("claude · seated");
  });
});

describe("missionDetailVM", () => {
  it("renders nothing before the payload arrives", () => {
    expect(missionDetailVM(undefined)).toBeNull();
  });

  it("consumes the three wire sections", () => {
    const vm = missionDetailVM(payload([thread()], [agent()]));
    expect(vm?.slug).toBe("mission-one");
    expect(vm?.title).toBe("Mission One");
    expect(vm?.threads).toHaveLength(1);
    expect(vm?.agents).toHaveLength(1);
    expect(vm?.rosterWarning).toBeNull();
  });

  it("derives the mission fact line: owner · status · created, empties dropped", () => {
    const full = missionDetailVM(payload([], []));
    expect(full?.facts).toBe("owner riley · active · 2026-07-15");
    const bare = payload([], []);
    bare.mission.status.owner = "";
    bare.mission.status.status = "";
    expect(missionDetailVM(bare)?.facts).toBe("2026-07-15");
    bare.mission.status.created = "";
    expect(missionDetailVM(bare)?.facts).toBeNull();
  });

  it("gap honesty: an unavailable board renders NO task summary on the detail page", () => {
    const withBoard = payload([], []);
    withBoard.mission.status.taskTotal = 4;
    expect(missionDetailVM(withBoard)?.taskSummary).toBe("4 tasks");
    const noBoard = payload([], []);
    noBoard.mission.status.boardAvailable = false;
    expect(missionDetailVM(noBoard)?.taskSummary).toBeNull();
  });

  it("sorts agents by name", () => {
    const vm = missionDetailVM(
      payload([], [agent({ name: "worker-suna" }), agent({ name: "builder-lobo" })]),
    );
    expect(vm?.agents.map((row) => row.name)).toEqual(["builder-lobo", "worker-suna"]);
  });

  it("surfaces the roster warning, empty string as null", () => {
    expect(missionDetailVM(payload([], [], "hcom unreachable"))?.rosterWarning).toBe(
      "hcom unreachable",
    );
  });
});
