import { createContext, useContext } from "react";
import { type Skin, type SkinName, skinNames } from "@/skins/contract";
import { minimalSkin } from "@/skins/minimal";
import { terminalSkin } from "@/skins/terminal";

export const skins: Record<SkinName, Skin> = {
  minimal: minimalSkin,
  terminal: terminalSkin,
};

export function isSkinName(value: string | null): value is SkinName {
  return value !== null && (skinNames as readonly string[]).includes(value);
}

// The skin is selected ONCE at the top of the tree (composition root) and
// consumed via context; no component picks its own skin.
export const SkinContext = createContext<Skin>(minimalSkin);

export function useSkin(): Skin {
  return useContext(SkinContext);
}
