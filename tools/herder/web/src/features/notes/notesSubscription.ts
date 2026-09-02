import type { NotesStore } from './notesStore'

export type NotesSelectorSignal<T> = {
  subscribe: (listener: () => void) => () => void
  getSnapshot: () => T
}

export function shallowEqualSnapshots<T>(left: readonly T[], right: readonly T[]) {
  return left.length === right.length && left.every((value, index) => Object.is(value, right[index]))
}

export function createNotesSelectorSignal<T>(
  subscribeSource: (listener: () => void) => () => void,
  read: () => T,
  equal: (left: T, right: T) => boolean = Object.is,
): NotesSelectorSignal<T> {
  let snapshot = read()
  let unsubscribeSource: (() => void) | undefined
  const listeners = new Set<() => void>()

  const refresh = () => {
    const next = read()
    if (equal(snapshot, next)) return
    snapshot = next
    for (const listener of listeners) listener()
  }

  return {
    getSnapshot: () => snapshot,
    subscribe: (listener) => {
      listeners.add(listener)
      if (!unsubscribeSource) {
        unsubscribeSource = subscribeSource(refresh)
        // A synchronous write may land between the render that created this
        // signal and React attaching its subscription. Reconcile only here;
        // getSnapshot itself stays referentially stable between notifications.
        refresh()
      }
      return () => {
        listeners.delete(listener)
        if (listeners.size > 0) return
        unsubscribeSource?.()
        unsubscribeSource = undefined
      }
    },
  }
}

export function allNotesSignal(store: NotesStore) {
  return createNotesSelectorSignal(store.subscribe, store.list, shallowEqualSnapshots)
}

export function groupNotesSignal(store: NotesStore, group: string) {
  return createNotesSelectorSignal(store.subscribe, () => store.listGroup(group), shallowEqualSnapshots)
}

export function notesGroupsSignal(store: NotesStore, groups: string[]) {
  const included = new Set(groups)
  return createNotesSelectorSignal(store.subscribe, () => store.list().filter((note) => included.has(note.group)), shallowEqualSnapshots)
}

export function notesStatusSignal(store: NotesStore) {
  return createNotesSelectorSignal(store.subscribe, store.status)
}

export function notesCountSignal(store: NotesStore) {
  return createNotesSelectorSignal(store.subscribe, () => store.list().length)
}
