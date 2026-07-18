import { useCallback, useEffect, useState } from "react";
import type { SkinName } from "@/skins/contract";
import { isSkinName } from "@/skins/index";

const STORAGE_KEY = "mc-skin";

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
    setName((current) => (current === "minimal" ? "terminal" : "minimal"));
  }, []);
  return [name, toggle];
}
