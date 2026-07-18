import { describe, expect, it } from "vitest";
import { createStore } from "zustand/vanilla";
import {
  applyOpenMaterial,
  applyToggleThread,
  MATERIAL_CAP,
  type MaterialSlot,
  workingSetCreator,
} from "@/stores/working-set";

// The store under test is the REAL creator (the same closed action set
// production wraps in persist), driven through zustand/vanilla.
function freshStore() {
  return createStore(workingSetCreator);
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
