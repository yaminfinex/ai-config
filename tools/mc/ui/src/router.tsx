import { createRootRoute, createRoute, createRouter } from "@tanstack/react-router";
import { MissionPage } from "@/routes/mission-page";
import { MissionsPage } from "@/routes/missions-page";
import { RootLayout } from "@/routes/root";

// The typed route table (D1): every core entity gets a shareable URL here.
// Code-based route tree — no codegen. The SPA mounts under /ui on the Go
// binary during the transition, hence the basepath.

const rootRoute = createRootRoute({ component: RootLayout });

export const missionsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: MissionsPage,
});

export const missionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/mission/$slug",
  component: MissionPage,
});

const routeTree = rootRoute.addChildren([missionsRoute, missionRoute]);

export const router = createRouter({ routeTree, basepath: "/ui" });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
