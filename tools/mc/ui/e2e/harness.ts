import { type ChildProcess, spawn } from "node:child_process";
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
  stop: () => void;
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

async function waitForServer(url: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    try {
      const res = await fetch(url);
      if (res.ok) {
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

/** Start the real mc binary on `port` with fake upstreams in `mish` mode. */
export async function startMc(port: number, mish: MishMode = "healthy"): Promise<McServer> {
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
    { stdio: "ignore" },
  );
  const baseUrl = `http://127.0.0.1:${port}`;
  // The readiness probe must not touch /api in slow/hang modes: an /api probe
  // triggers a mish observation whose result the resolver caches (or, hung,
  // never returns), and the page under test would then load instantly instead
  // of showing its honest loading state. /ui/ proves the process is serving
  // without warming caches.
  const probePath = mish === "slow" || mish === "hang" ? "/ui/" : "/api/v1/version";
  try {
    await waitForServer(`${baseUrl}${probePath}`, 20_000);
  } catch (err) {
    child.kill();
    throw err;
  }
  return {
    baseUrl,
    flip: (name) => writeFileSync(join(tmp, name), ""),
    unflip: (name) => rmSync(join(tmp, name), { force: true }),
    stop: () => {
      child.kill();
      rmSync(tmp, { recursive: true, force: true });
    },
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
export function startDeadShell(port: number): { baseUrl: string; stop: () => void } {
  const dist = join(uiDir, "dist");
  const server: Server = createServer((req, res) => {
    const url = new URL(req.url ?? "/", `http://127.0.0.1:${port}`);
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
  server.listen(port, "127.0.0.1");
  return {
    baseUrl: `http://127.0.0.1:${port}`,
    stop: () => server.close(),
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
