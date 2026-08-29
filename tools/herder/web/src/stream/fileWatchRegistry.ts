import { createContext, useContext, useEffect } from 'react'

export type FileWatchTarget = {
  kind: 'file' | 'folder'
  root: string
  path: string
}

type TimerHost = Pick<typeof window, 'setTimeout' | 'clearTimeout'>
type Registration = { target: FileWatchTarget, references: number }

export type FileWatchRegistry = {
  register: (target: FileWatchTarget) => () => void
  dispose: () => void
}

function targetKey(target: FileWatchTarget) {
  return JSON.stringify([target.kind, target.root, target.path])
}

export function createFileWatchRegistry(
  onSnapshot: (targets: FileWatchTarget[]) => void,
  timers: TimerHost = window,
  delay = 50,
): FileWatchRegistry {
  const registrations = new Map<string, Registration>()
  let timer: number | null = null
  let disposed = false
  let lastSnapshot = '[]'
  const schedule = () => {
    if (disposed) return
    if (timer !== null) timers.clearTimeout(timer)
    timer = timers.setTimeout(() => {
      timer = null
      const targets = [...registrations.values()].map(({ target }) => target)
      const serialized = JSON.stringify(targets)
      if (serialized === lastSnapshot) return
      lastSnapshot = serialized
      onSnapshot(targets)
    }, delay)
  }
  return {
    register(target) {
      if (disposed) return () => undefined
      const key = targetKey(target)
      const current = registrations.get(key)
      if (current) current.references++
      else registrations.set(key, { target, references: 1 })
      schedule()
      let active = true
      return () => {
        if (!active || disposed) return
        active = false
        const registered = registrations.get(key)
        if (!registered) return
        registered.references--
        if (registered.references === 0) registrations.delete(key)
        schedule()
      }
    },
    dispose() {
      disposed = true
      registrations.clear()
      if (timer !== null) timers.clearTimeout(timer)
      timer = null
    },
  }
}

export const FileWatchContext = createContext<FileWatchRegistry['register'] | null>(null)

export function useFileWatch(target: FileWatchTarget, enabled = true) {
  const register = useContext(FileWatchContext)
  useEffect(() => {
    if (!enabled || !register) return
    return register(target)
  }, [enabled, register, target.kind, target.path, target.root])
}
