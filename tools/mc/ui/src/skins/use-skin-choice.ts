import { useCallback, useEffect, useState } from "react";
import { type SkinName, skinNames } from "@/skins/contract";
import { isSkinName } from "@/skins/index";

const STORAGE_KEY = "mc-skin";

/**
 * The toggle cycle, pure: the next skin in `skinNames` order, wrapping.
 * `skinNames` is the single enumeration source (§3) — hardcoding any skin
 * pair here would silently drop a third skin from the toggle.
 */
export function nextSkin(current: SkinName): SkinName {
  return skinNames[(skinNames.indexOf(current) + 1) % skinNames.length] ?? current;
}

// Skin choice is preference, not working-set layout: it lives beside the
// working-set store, not in it. It survives refresh via localStorage and
// drives BOTH halves of the swap — data-theme (tokens) and the component set.
export function useSkinChoice(): [SkinName, () => void] {
  const [name, setName] = useState<SkinName>(() => {
    const stored = localStorage.getItem(STORAGE_KEY);
    return isSkinName(stored) ? stored : "minimal";
  });
  useEffect(() => {
    document.documentElement.dataset.theme = name;
    localStorage.setItem(STORAGE_KEY, name);
  }, [name]);
  const toggle = useCallback(() => {
    setName(nextSkin);
  }, []);
  return [name, toggle];
}
