import { describe, expect, it } from "vitest";
import { createJSONStorage, persist } from "zustand/middleware";
import { createStore } from "zustand/vanilla";
import {
  applyOpenMaterial,
  applyToggleThread,
  MATERIAL_CAP,
  type MaterialSlot,
  normalizeWorkingSetState,
  type WorkingSet,
  workingSetCreator,
  workingSetPersistOptions,
} from "@/stores/working-set";

// The store under test is the REAL creator (the same closed action set
// production wraps in persist), driven through zustand/vanilla.
function freshStore() {
  return createStore(workingSetCreator);
}

// Rehydration under test goes through the REAL persist options — creator,
// merge, migrate, version — with only the storage swapped for a shim
// holding poisoned state.
function rehydratedStore(persistedState: unknown, version = workingSetPersistOptions.version) {
  const items = new Map<string, string>([
    ["mc-working-set", JSON.stringify({ state: persistedState, version })],
  ]);
  const storage = {
    getItem: (key: string) => items.get(key) ?? null,
    setItem: (key: string, value: string) => void items.set(key, value),
    removeItem: (key: string) => void items.delete(key),
  };
  return createStore<WorkingSet>()(
    persist(workingSetCreator, {
      ...workingSetPersistOptions,
      storage: createJSONStorage(() => storage),
    }),
  );
}

describe("navigate", () => {
  it("changes the view and NOTHING else — navigation never grows the layout", () => {
    const store = freshStore();
    store.getState().toggleThread("t-1");
    store.getState().openMaterial("doc-a");
    store.getState().pinMaterial("doc-a");
    const { thread, materials } = store.getState();

    store.getState().navigate({ kind: "mission", slug: "mission-one" });

    expect(store.getState().view).toEqual({ kind: "mission", slug: "mission-one" });
    expect(store.getState().thread).toEqual(thread);
    expect(store.getState().materials).toEqual(materials);
  });
});

describe("the active thread slot", () => {
  it("replaces by default and never multiplies", () => {
    const store = freshStore();
    store.getState().toggleThread("t-1");
    expect(store.getState().thread).toEqual({ id: "t-1" });
    store.getState().toggleThread("t-2");
    expect(store.getState().thread).toEqual({ id: "t-2" });
  });

  it("toggles closed on the same id", () => {
    const store = freshStore();
    store.getState().toggleThread("t-1");
    store.getState().toggleThread("t-1");
    expect(store.getState().thread).toBeNull();
  });

  it("closes explicitly", () => {
    const store = freshStore();
    store.getState().toggleThread("t-1");
    store.getState().closeThread();
    expect(store.getState().thread).toBeNull();
  });

  it("applyToggleThread is the same law, pure", () => {
    expect(applyToggleThread(null, "t-1")).toEqual({ id: "t-1" });
    expect(applyToggleThread({ id: "t-1" }, "t-2")).toEqual({ id: "t-2" });
    expect(applyToggleThread({ id: "t-1" }, "t-1")).toBeNull();
  });
});

describe("material slots", () => {
  it("open replaces the newest unpinned slot by default", () => {
    const store = freshStore();
    store.getState().openMaterial("doc-a");
    store.getState().openMaterial("doc-b");
    expect(store.getState().materials).toEqual([{ ref: "doc-b", pinned: false }]);
  });

  it("pin is the only grow gesture", () => {
    const store = freshStore();
    store.getState().openMaterial("doc-a");
    store.getState().pinMaterial("doc-a");
    store.getState().openMaterial("doc-b");
    expect(store.getState().materials).toEqual([
      { ref: "doc-a", pinned: true },
      { ref: "doc-b", pinned: false },
    ]);
  });

  it("at cap with everything pinned, opening refuses", () => {
    const store = freshStore();
    store.getState().openMaterial("doc-a");
    store.getState().pinMaterial("doc-a");
    store.getState().openMaterial("doc-b");
    store.getState().pinMaterial("doc-b");
    store.getState().openMaterial("doc-c");
    expect(store.getState().materials).toEqual([
      { ref: "doc-a", pinned: true },
      { ref: "doc-b", pinned: true },
    ]);
  });

  it("opening an already-open ref changes nothing", () => {
    const store = freshStore();
    store.getState().openMaterial("doc-a");
    store.getState().pinMaterial("doc-a");
    store.getState().openMaterial("doc-a");
    expect(store.getState().materials).toEqual([{ ref: "doc-a", pinned: true }]);
  });

  it("close removes one slot", () => {
    const store = freshStore();
    store.getState().openMaterial("doc-a");
    store.getState().pinMaterial("doc-a");
    store.getState().openMaterial("doc-b");
    store.getState().closeMaterial("doc-a");
    expect(store.getState().materials).toEqual([{ ref: "doc-b", pinned: false }]);
  });

  it("clicking around cannot exceed the cap", () => {
    const store = freshStore();
    for (let i = 0; i < 20; i++) {
      store.getState().openMaterial(`doc-${i}`);
      if (i % 3 === 0) {
        store.getState().pinMaterial(`doc-${i}`);
      }
      expect(store.getState().materials.length).toBeLessThanOrEqual(MATERIAL_CAP);
    }
  });

  it("applyOpenMaterial replaces the NEWEST unpinned slot, not the oldest", () => {
    const materials: MaterialSlot[] = [
      { ref: "doc-a", pinned: false },
      { ref: "doc-b", pinned: false },
    ];
    expect(applyOpenMaterial(materials, "doc-c")).toEqual([
      { ref: "doc-a", pinned: false },
      { ref: "doc-c", pinned: false },
    ]);
  });
});

describe("persist rehydration — the wall holds where production constructs state", () => {
  it("a legal persisted state survives intact", () => {
    const store = rehydratedStore({
      view: { kind: "mission", slug: "mission-one" },
      thread: { id: "t-1" },
      materials: [
        { ref: "doc-a", pinned: true },
        { ref: "doc-b", pinned: false },
      ],
    });
    expect(store.getState().view).toEqual({ kind: "mission", slug: "mission-one" });
    expect(store.getState().thread).toEqual({ id: "t-1" });
    expect(store.getState().materials).toEqual([
      { ref: "doc-a", pinned: true },
      { ref: "doc-b", pinned: false },
    ]);
  });

  it("an over-cap materials array is clamped, keeping the deliberate (pinned) slots", () => {
    const store = rehydratedStore({
      view: { kind: "missions" },
      thread: null,
      materials: [
        { ref: "doc-a", pinned: false },
        { ref: "doc-b", pinned: true },
        { ref: "doc-c", pinned: false },
        { ref: "doc-d", pinned: true },
      ],
    });
    expect(store.getState().materials).toEqual([
      { ref: "doc-b", pinned: true },
      { ref: "doc-d", pinned: true },
    ]);
  });

  it("malformed slots, duplicate refs, and junk shapes are dropped", () => {
    const store = rehydratedStore({
      view: { kind: "mission" }, // missing slug → illegal
      thread: { id: 42 },
      materials: [
        { ref: 7, pinned: true },
        { ref: "ok", pinned: "yes" },
        null,
        "doc",
        { ref: "doc-a", pinned: true },
        { ref: "doc-a", pinned: false },
      ],
    });
    expect(store.getState().view).toEqual({ kind: "missions" });
    expect(store.getState().thread).toBeNull();
    expect(store.getState().materials).toEqual([{ ref: "doc-a", pinned: true }]);
  });

  it("garbage at the top level rehydrates to the defaults", () => {
    const store = rehydratedStore("not even an object");
    expect(store.getState().view).toEqual({ kind: "missions" });
    expect(store.getState().thread).toBeNull();
    expect(store.getState().materials).toEqual([]);
  });

  it("state written under an older version is migrated through the same normalization", () => {
    const store = rehydratedStore(
      {
        view: { kind: "mission", slug: "mission-one" },
        materials: [
          { ref: "doc-a", pinned: true },
          { ref: "doc-b", pinned: true },
          { ref: "doc-c", pinned: true },
        ],
      },
      0,
    );
    expect(store.getState().view).toEqual({ kind: "mission", slug: "mission-one" });
    expect(store.getState().materials).toHaveLength(MATERIAL_CAP);
  });

  it("the closed action set still governs after a poisoned rehydration", () => {
    const store = rehydratedStore({
      materials: [
        { ref: "doc-a", pinned: true },
        { ref: "doc-b", pinned: true },
        { ref: "doc-c", pinned: true },
      ],
    });
    store.getState().openMaterial("doc-z");
    expect(store.getState().materials).toEqual([
      { ref: "doc-a", pinned: true },
      { ref: "doc-b", pinned: true },
    ]);
  });
});

describe("normalizeWorkingSetState", () => {
  it("never returns more than the cap regardless of input size", () => {
    const flood = Array.from({ length: 50 }, (_, i) => ({ ref: `doc-${i}`, pinned: i % 2 === 0 }));
    expect(normalizeWorkingSetState({ materials: flood }).materials).toHaveLength(MATERIAL_CAP);
  });
});
