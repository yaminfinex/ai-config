import { Link, Outlet } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { SkinContext, skins } from "@/skins/index";
import { useSkinChoice } from "@/skins/use-skin-choice";

// Composition root: selects the skin (both halves) and owns the shared
// chrome. Route components below wire entity hooks + view-models +
// working-set actions into skin components. Version-poll-driven cache
// invalidation is not wired yet: it mounts here once the entity layer
// grows its scope-aware invalidation hooks (ARCHITECTURE.md §"The wire
// contract"); until then pages refetch on mount/focus only.

export function RootLayout() {
  const [skinName, toggleSkin] = useSkinChoice();
  return (
    <SkinContext.Provider value={skins[skinName]}>
      <div className="min-h-screen">
        <header className="flex items-center justify-between border-b px-4 py-2">
          <Link to="/" className="font-fact text-sm">
            mc · ui
          </Link>
          <Button
            type="button"
            variant="outline"
            size="sm"
            data-testid="skin-toggle"
            onClick={toggleSkin}
          >
            skin: {skinName}
          </Button>
        </header>
        <main>
          <Outlet />
        </main>
      </div>
    </SkinContext.Provider>
  );
}
