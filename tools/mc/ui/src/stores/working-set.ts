import { create } from "zustand";
import { persist } from "zustand/middleware";

// The ratified working-set object (D2): one current view, at most one active
// thread, a hard-capped set of material slots. The invariants are transition
// rules, so they live here as a closed set of actions — views dispatch these
// and never mutate layout state any other way. Persisted client-local
// (implementation law: a page refresh loses nothing; layout never rides a URL).

export type ViewRef = { kind: "missions" } | { kind: "mission"; slug: string };

export interface ThreadRef {
  id: string;
}

export interface MaterialSlot {
  ref: string;
  pinned: boolean;
}

export const MATERIAL_CAP = 2;

export interface WorkingSetState {
  view: ViewRef;
  thread: ThreadRef | null;
  materials: MaterialSlot[];
}

interface WorkingSetActions {
  /** Navigation changes the view and NOTHING else — it never grows the layout. */
  navigate: (view: ViewRef) => void;
  /** The active thread slot replaces by default and never multiplies. */
  toggleThread: (id: string) => void;
  closeThread: () => void;
  /** Replace-by-default: following a reference replaces an unpinned slot. */
  openMaterial: (ref: string) => void;
  /** Pin is the deliberate grow gesture — the only one. */
  pinMaterial: (ref: string) => void;
  closeMaterial: (ref: string) => void;
}

/** Pure transition rule, exported for invariant tests. */
export function applyOpenMaterial(materials: MaterialSlot[], ref: string): MaterialSlot[] {
  if (materials.some((slot) => slot.ref === ref)) {
    return materials;
  }
  const replaceable = materials.findLastIndex((slot) => !slot.pinned);
  if (replaceable >= 0) {
    const next = [...materials];
    next[replaceable] = { ref, pinned: false };
    return next;
  }
  if (materials.length < MATERIAL_CAP) {
    return [...materials, { ref, pinned: false }];
  }
  // At cap with everything pinned: opening refuses; unpinning is the gesture.
  return materials;
}

export function applyToggleThread(thread: ThreadRef | null, id: string): ThreadRef | null {
  return thread?.id === id ? null : { id };
}

export const useWorkingSet = create<WorkingSetState & WorkingSetActions>()(
  persist(
    (set) => ({
      view: { kind: "missions" },
      thread: null,
      materials: [],
      navigate: (view) => set({ view }),
      toggleThread: (id) => set((state) => ({ thread: applyToggleThread(state.thread, id) })),
      closeThread: () => set({ thread: null }),
      openMaterial: (ref) => set((state) => ({ materials: applyOpenMaterial(state.materials, ref) })),
      pinMaterial: (ref) =>
        set((state) => ({
          materials: state.materials.map((slot) =>
            slot.ref === ref ? { ...slot, pinned: true } : slot,
          ),
        })),
      closeMaterial: (ref) =>
        set((state) => ({ materials: state.materials.filter((slot) => slot.ref !== ref) })),
    }),
    { name: "mc-working-set" },
  ),
);
