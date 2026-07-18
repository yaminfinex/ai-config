import { Link, Outlet } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { SkinContext, skins } from "@/skins/index";
import { useSkinChoice } from "@/skins/use-skin-choice";

// Composition root: selects the skin (both halves) and owns the shared
// chrome. Route components below wire entity hooks + view-models +
// working-set actions into skin components — including version-poll
// invalidation, which each page mounts with its own scope (the scope a
// page polls with mirrors what it renders, so it cannot live here).

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
