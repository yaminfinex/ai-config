import { type ChildProcess, execSync, spawn } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  createReadStream,
  existsSync,
  mkdirSync,
  mkdtempSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { createServer, type Server } from "node:http";
import { type AddressInfo, createServer as createTcpServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, type Page } from "@playwright/test";

// The harness of ARCHITECTURE.md §8: the REAL mc binary (built by
// global-setup, SPA embedded) with fake mish/hcom/herder shell scripts and a
// seeded journal fixture — the same pattern api_test.go and tools/mc/testdata
// use. Tests drive the served app over real HTTP; nothing is mocked in the
// frontend and no request is intercepted.

const e2eDir = dirname(fileURLToPath(import.meta.url));
const uiDir = join(e2eDir, "..");
const mcTestdata = join(uiDir, "..", "testdata");
const fixtures = join(e2eDir, "fixtures");
const binary = join(e2eDir, ".build", "mc-e2e");

/**
 * What the fake mish reports:
 * - healthy: two missions (mission-one healthy, mission-broken degraded),
 *   per-slug detail fixtures, unknown slugs refused ("ghost" fixture).
 * - empty: an observed zero — `--all` returns [] with exit 0.
 * - degraded: the source is unobservable — every invocation fails.
 * - slow: healthy, after a 2s pause — makes the loading→data transition
 *   observable.
 * - hang: never answers — holds the page in its loading state so the full
 *   claim set of that state can be asserted without racing data arrival.
 */
export type MishMode = "healthy" | "empty" | "degraded" | "slow" | "hang";

export interface McServer {
  baseUrl: string;
  /** Create/remove a flip file the fake upstreams check (e.g. "herder-down"). */
  flip: (name: string) => void;
  unflip: (name: string) => void;
  /** Kills the WHOLE process tree and throws if any of it survives. */
  stop: () => Promise<void>;
}

function mishDispatch(): string {
  // Per-slug detail fixtures: mission-one is deliberately warning-free (the
  // shared testdata variant carries an artifacts warning, which would make
  // the "healthy data" state's full-claim-set assertions meaningless);
  // mission-broken and ghost carry the degradation/refusal states.
  return `case "$*" in
  *"--all"*) cat "${mcTestdata}/status-all.json" ;;
  *ghost*) cat "${mcTestdata}/status-mission-not-found.json" ;;
  *mission-broken*) cat "${fixtures}/status-mission-broken.json" ;;
  *) cat "${fixtures}/status-mission-one.json" ;;
esac`;
}

function mishScript(mode: MishMode): string {
  switch (mode) {
    case "healthy":
      return `#!/bin/sh\n${mishDispatch()}\n`;
    case "empty":
      return `#!/bin/sh\ncase "$*" in\n  *"--all"*) printf '[]' ;;\n  *) cat "${mcTestdata}/status-mission-not-found.json" ;;\nesac\n`;
    case "degraded":
      return `#!/bin/sh\necho 'mish: missions repo unreachable' >&2\nexit 1\n`;
    case "slow":
      return `#!/bin/sh\nsleep 2\n${mishDispatch()}\n`;
    case "hang":
      return `#!/bin/sh\nsleep 600\n`;
  }
}

const herderSession = JSON.stringify({
  kind: "session",
  label: "builder-lobo",
  guid: "g-1",
  tool: "claude",
  role: "builder",
  state: "seated",
  cwd: "/work",
  branch: "task-x",
  mission: { slug: "mission-one", source: "marker" },
});

/**
 * Ask the OS for a free ephemeral port. Fixed ports collide when suites (or
 * concurrent gate runs) overlap; ephemeral ones cannot. The port is released
 * before the server binds it, so a rare steal is possible — that surfaces as
 * an honest startup failure in waitForServer, never a silent collision.
 */
function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const probe = createTcpServer();
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const { port } = probe.address() as AddressInfo;
      probe.close((err) => (err ? reject(err) : resolve(port)));
    });
  });
}

/** Live PIDs in a process group; empty when the group is fully gone. */
function groupPids(pgid: number): string[] {
  try {
    return execSync(`ps -o pid= -g ${pgid}`, { encoding: "utf8" })
      .split("\n")
      .map((line) => line.trim())
      .filter((line) => line !== "");
  } catch {
    return []; // ps exits non-zero when the group has no processes
  }
}

/**
 * Readiness is a 2xx from the address AND the spawned server still alive —
 * a 2xx alone could be someone else's server if the ephemeral port was
 * stolen between release and bind. A lost bind exits the child, so
 * `assertAlive` (checked each attempt and again at the 2xx) turns that
 * steal into the honest startup failure it is instead of coupling the
 * suite onto the thief.
 */
async function waitForServer(
  url: string,
  timeoutMs: number,
  assertAlive: () => void,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    assertAlive();
    try {
      const res = await fetch(url);
      if (res.ok) {
        assertAlive(); // the 2xx only counts as OURS with the child alive
        return;
      }
    } catch {
      // not up yet
    }
    if (Date.now() > deadline) {
      throw new Error(`server at ${url} did not come up within ${timeoutMs}ms`);
    }
    await new Promise((resolve) => setTimeout(resolve, 150));
  }
}

/** Start the real mc binary on an ephemeral port with fake upstreams in `mish` mode. */
export async function startMc(mish: MishMode = "healthy"): Promise<McServer> {
  const port = await freePort();
  const tmp = mkdtempSync(join(tmpdir(), "mc-e2e-"));
  const bin = join(tmp, "bin");
  mkdirSync(bin);
  const write = (name: string, content: string) => {
    const p = join(bin, name);
    writeFileSync(p, content);
    chmodSync(p, 0o755);
    return p;
  };
  const mishBin = write("mish", mishScript(mish));
  const hcomBin = write(
    "hcom",
    `#!/bin/sh\ncase "$1" in\n  list) printf '[]' ;;\n  *) : ;;\nesac\n`,
  );
  const herderBin = write(
    "herder",
    `#!/bin/sh\n[ -f "${tmp}/herder-down" ] && { echo 'herder: down' >&2; exit 1; }\nprintf '%s\\n' '${herderSession}'\n`,
  );
  copyFileSync(join(fixtures, "journal.jsonl"), join(tmp, "journal.jsonl"));

  // detached: the server becomes its own process-group leader, so stop()
  // can signal the WHOLE tree — mc plus whatever fake upstreams it has in
  // flight (the hang fixture's sh+sleep would otherwise outlive the parent,
  // reparented to PID 1, and pollute the box run after run).
  const child: ChildProcess = spawn(
    binary,
    [
      "--addr",
      `127.0.0.1:${port}`,
      "--no-seat",
      "--journal",
      join(tmp, "journal.jsonl"),
      "--mish",
      mishBin,
      "--hcom",
      hcomBin,
      "--herder",
      herderBin,
    ],
    { stdio: "ignore", detached: true },
  );
  const stop = async (): Promise<void> => {
    const pid = child.pid;
    if (pid !== undefined) {
      try {
        process.kill(-pid, "SIGKILL"); // the whole group, not just the parent
      } catch {
        // group already gone
      }
      // The teardown ASSERTS the tree is gone: a future leak fails the suite
      // instead of stranding orphans on the box.
      const deadline = Date.now() + 5_000;
      for (;;) {
        const alive = groupPids(pid);
        if (alive.length === 0) {
          break;
        }
        if (Date.now() > deadline) {
          throw new Error(`mc process group ${pid} survived stop(): pids ${alive.join(", ")}`);
        }
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
    }
    rmSync(tmp, { recursive: true, force: true });
  };

  const baseUrl = `http://127.0.0.1:${port}`;
  // The readiness probe must not touch /api in slow/hang modes: an /api probe
  // triggers a mish observation whose result the resolver caches (or, hung,
  // never returns), and the page under test would then load instantly instead
  // of showing its honest loading state. /ui/ proves the process is serving
  // without warming caches.
  const probePath = mish === "slow" || mish === "hang" ? "/ui/" : "/api/v1/version";
  const assertAlive = () => {
    if (child.exitCode !== null || child.signalCode !== null) {
      throw new Error(
        `mc exited during startup (${child.signalCode ?? `code ${child.exitCode}`}) — ` +
          `a lost port bind or a crash; any response from ${baseUrl} is not ours`,
      );
    }
  };
  try {
    await waitForServer(`${baseUrl}${probePath}`, 20_000, assertAlive);
  } catch (err) {
    await stop();
    throw err;
  }
  return {
    baseUrl,
    flip: (name) => writeFileSync(join(tmp, name), ""),
    unflip: (name) => rmSync(join(tmp, name), { force: true }),
    stop,
  };
}

const contentTypes: Record<string, string> = {
  ".html": "text/html",
  ".js": "text/javascript",
  ".css": "text/css",
  ".svg": "image/svg+xml",
};

/**
 * The cold-dead-server page: the built SPA shell served statically while every
 * /api response is 503 — the state a browser sees when the backend is down
 * but the page is reachable (or cached). Real HTTP end to end; the 503 is a
 * genuine wire response, not interception.
 */
export async function startDeadShell(): Promise<{ baseUrl: string; stop: () => Promise<void> }> {
  const dist = join(uiDir, "dist");
  const server: Server = createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://127.0.0.1");
    if (url.pathname.startsWith("/api/")) {
      res.writeHead(503).end("api down");
      return;
    }
    const asset = url.pathname.startsWith("/ui/assets/")
      ? join(dist, normalize(url.pathname.slice(3)))
      : join(dist, "index.html");
    if (!asset.startsWith(dist) || !existsSync(asset)) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, {
      "content-type": contentTypes[extname(asset)] ?? "application/octet-stream",
    });
    createReadStream(asset).pipe(res);
  });
  // Ephemeral bind, awaited: the OS picks the port and the promise resolves
  // only once the shell is actually accepting connections.
  const port = await new Promise<number>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      resolve((server.address() as AddressInfo).port);
    });
  });
  return {
    baseUrl: `http://127.0.0.1:${port}`,
    // Awaited close, with sockets destroyed first — a browser's keep-alive
    // connections would otherwise hold `close` open past the test.
    stop: () =>
      new Promise<void>((resolve, reject) => {
        server.close((err) => (err ? reject(err) : resolve()));
        server.closeAllConnections();
      }),
  };
}

export const allSkins = ["minimal", "terminal"] as const;
export type SkinUnderTest = (typeof allSkins)[number];

// ---------------------------------------------------------------------------
// Full-claim-set assertions. The render precedence (ARCHITECTURE.md §6,
// failure > loading > empty claim > data, staleness beside data) is mutual
// exclusivity, so every state flow asserts EVERY claim: present only where
// the law puts it, count 0 in every other state. A regression rendering
// loading beside "no missions", or a failure line beside data, fails here —
// not just in the state it belongs to. Present claims are asserted first so
// Playwright's auto-waiting settles the state before the exclusions run.

async function expectClaims(
  page: Page,
  claims: Record<string, boolean>,
  counts: Record<string, number>,
): Promise<void> {
  const ordered = Object.entries(claims).sort(([, a], [, b]) => Number(b) - Number(a));
  for (const [testid, present] of ordered) {
    if (present) {
      await expect(page.getByTestId(testid).first()).toBeVisible();
    } else {
      await expect(page.getByTestId(testid)).toHaveCount(0);
    }
  }
  for (const [testid, count] of Object.entries(counts)) {
    await expect(page.getByTestId(testid)).toHaveCount(count);
  }
}

export interface ListPageState {
  rows?: number;
  failure?: boolean;
  loading?: boolean;
  empty?: boolean;
  stale?: boolean;
  warning?: boolean;
}

/** Assert the COMPLETE claim set of the missions-list page. */
export async function expectListPageState(page: Page, state: ListPageState): Promise<void> {
  await expectClaims(
    page,
    {
      "load-failure": state.failure ?? false,
      loading: state.loading ?? false,
      "missions-empty": state.empty ?? false,
      "stale-warning": state.stale ?? false,
      "list-warning": state.warning ?? false,
    },
    { "mission-row": state.rows ?? 0 },
  );
}

export interface DetailPageState {
  facts?: boolean;
  taskSummary?: boolean;
  threadRows?: number;
  agentRows?: number;
  failure?: boolean;
  loading?: boolean;
  stale?: boolean;
  missionWarning?: boolean;
  rosterWarning?: boolean;
  threadsEmpty?: boolean;
  crewEmpty?: boolean;
}

/** Assert the COMPLETE claim set of the mission page. */
export async function expectDetailPageState(page: Page, state: DetailPageState): Promise<void> {
  await expectClaims(
    page,
    {
      "load-failure": state.failure ?? false,
      loading: state.loading ?? false,
      "stale-warning": state.stale ?? false,
      "mission-warning": state.missionWarning ?? false,
      "roster-warning": state.rosterWarning ?? false,
      "threads-empty": state.threadsEmpty ?? false,
      "crew-empty": state.crewEmpty ?? false,
      "mission-facts": state.facts ?? false,
      "task-summary": state.taskSummary ?? false,
    },
    { "thread-row": state.threadRows ?? 0, "agent-row": state.agentRows ?? 0 },
  );
}

/** Select the skin before the app boots — the persisted preference the
 * composition root reads. The flow then runs identically under each skin. */
export async function useSkin(page: Page, skin: SkinUnderTest): Promise<void> {
  await page.addInitScript((name) => {
    window.localStorage.setItem("mc-skin", name);
  }, skin);
}
