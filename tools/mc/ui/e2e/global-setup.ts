import { execSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// Build what the suite drives: the production SPA bundle and the real Go
// binary that embeds and serves it. The suite never runs against the Vite
// dev server — production wiring (go:embed, SPA fallback, /api) is part of
// what the flows verify.

const uiDir = join(dirname(fileURLToPath(import.meta.url)), "..");
const mcDir = join(uiDir, "..");
export const E2E_BINARY = join(uiDir, "e2e", ".build", "mc-e2e");

export default function globalSetup(): void {
  execSync("bun run build", { cwd: uiDir, stdio: "inherit" });
  // vite build empties dist/; the keep file makes go:embed valid on unbuilt
  // checkouts and must survive the suite (README).
  execSync("git checkout -- dist/.gitkeep", { cwd: uiDir, stdio: "inherit" });
  mkdirSync(dirname(E2E_BINARY), { recursive: true });
  execSync(`GOTOOLCHAIN=local mise exec go@1.26.5 -- go build -o ${JSON.stringify(E2E_BINARY)} .`, {
    cwd: mcDir,
    stdio: "inherit",
  });
}
