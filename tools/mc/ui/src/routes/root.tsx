import { Link, Outlet } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { useVersionInvalidation } from "@/entities/version";
import { SkinContext, skins } from "@/skins/index";
import { useSkinChoice } from "@/skins/use-skin-choice";

// Composition root: selects the skin (both halves), owns the chrome, and
// wires version-driven cache invalidation. Route components below wire
// entity hooks + view-models + working-set actions into skin components.

export function RootLayout() {
  useVersionInvalidation();
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
