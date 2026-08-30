import { useCallback, useState } from 'react'

export type PanelRecordUpdate<T> = T | undefined | ((previous: T | undefined) => T | undefined)

export function updatePanelRecord<T>(
  current: Record<string, T>,
  key: string,
  update: PanelRecordUpdate<T>,
  equal: (left: T, right: T) => boolean = Object.is,
) {
  const previous = current[key]
  const nextValue = typeof update === 'function'
    ? (update as (value: T | undefined) => T | undefined)(previous)
    : update
  if (nextValue === undefined) {
    if (!(key in current)) return current
    const next = { ...current }
    delete next[key]
    return next
  }
  return previous !== undefined && equal(previous, nextValue) ? current : { ...current, [key]: nextValue }
}

export function usePanelRecords<T>(equal: (left: T, right: T) => boolean = Object.is) {
  const [records, setRecords] = useState<Record<string, T>>({})
  const set = useCallback((key: string, update: PanelRecordUpdate<T>) => {
    setRecords((current) => updatePanelRecord(current, key, update, equal))
  }, [equal])
  const prune = useCallback((key: string) => setRecords((current) => updatePanelRecord(current, key, undefined, equal)), [equal])
  return { records, set, prune }
}
