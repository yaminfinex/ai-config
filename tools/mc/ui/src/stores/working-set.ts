import { create, type StateCreator } from "zustand";
import { type PersistOptions, persist } from "zustand/middleware";

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

export type WorkingSet = WorkingSetState & WorkingSetActions;

// The closed action set, exported bare so invariant tests drive the REAL
// transitions through zustand/vanilla without the persist middleware (and
// its localStorage dependency). Production wraps it below; the behaviour
// under test and the behaviour shipped are the same function.
export const workingSetCreator: StateCreator<WorkingSet> = (set) => ({
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
});

// --- rehydration discipline ----------------------------------------------
//
// The cap and shape invariants must hold where production actually
// constructs state, and persist rehydration is such a place: localStorage
// is outside the closed action set (older builds, other tabs, hand edits),
// so whatever comes back is validated and normalized into LEGAL state
// before it becomes the store. A shallow merge of trusted-as-is JSON would
// let an over-cap or malformed materials array walk straight past the wall
// the actions enforce.

function normalizeView(value: unknown): ViewRef {
  if (typeof value === "object" && value !== null) {
    const raw = value as Record<string, unknown>;
    if (raw.kind === "missions") {
      return { kind: "missions" };
    }
    if (raw.kind === "mission" && typeof raw.slug === "string" && raw.slug !== "") {
      return { kind: "mission", slug: raw.slug };
    }
  }
  return { kind: "missions" };
}

function normalizeThread(value: unknown): ThreadRef | null {
  if (typeof value === "object" && value !== null) {
    const raw = value as Record<string, unknown>;
    if (typeof raw.id === "string" && raw.id !== "") {
      return { id: raw.id };
    }
  }
  return null;
}

function normalizeMaterials(value: unknown): MaterialSlot[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const legal: MaterialSlot[] = [];
  for (const item of value) {
    if (typeof item !== "object" || item === null) {
      continue;
    }
    const raw = item as Record<string, unknown>;
    if (
      typeof raw.ref === "string" &&
      raw.ref !== "" &&
      typeof raw.pinned === "boolean" &&
      !legal.some((slot) => slot.ref === raw.ref)
    ) {
      legal.push({ ref: raw.ref, pinned: raw.pinned });
    }
  }
  if (legal.length <= MATERIAL_CAP) {
    return legal;
  }
  // Over cap: pinned slots are the deliberate gestures — drop the
  // undeliberate first, preserving order within each group.
  const pinned = legal.filter((slot) => slot.pinned);
  const unpinned = legal.filter((slot) => !slot.pinned);
  return [...pinned, ...unpinned].slice(0, MATERIAL_CAP);
}

/** Any persisted value → legal working-set state. Exported for tests. */
export function normalizeWorkingSetState(persisted: unknown): WorkingSetState {
  if (typeof persisted !== "object" || persisted === null) {
    return { view: { kind: "missions" }, thread: null, materials: [] };
  }
  const raw = persisted as Record<string, unknown>;
  return {
    view: normalizeView(raw.view),
    thread: normalizeThread(raw.thread),
    materials: normalizeMaterials(raw.materials),
  };
}

// Exported so rehydration tests run through the REAL production persist
// configuration (with a storage shim), not a copy of it.
export const workingSetPersistOptions: PersistOptions<WorkingSet, WorkingSetState> = {
  name: "mc-working-set",
  version: 1,
  partialize: (state) => ({
    view: state.view,
    thread: state.thread,
    materials: state.materials,
  }),
  // merge runs on every rehydration; migrate additionally covers persisted
  // state written under an older version number. Both funnel through the
  // same normalization — there is no path from storage into the store that
  // skips it.
  merge: (persisted, current) => ({ ...current, ...normalizeWorkingSetState(persisted) }),
  migrate: (persisted) => normalizeWorkingSetState(persisted),
};

export const useWorkingSet = create<WorkingSet>()(
  persist(workingSetCreator, workingSetPersistOptions),
);
