export type RailSide = 'left' | 'right'

export type RailPreference = {
  width: number
  collapsed: boolean
}

export type RailPreferences = {
  fleet: RailPreference
  notes: RailPreference
}

export const minimumRailWidth = 200
export const maximumRailWidth = 440
export const defaultFleetRailWidth = 250
export const defaultNotesRailWidth = 280

export function clampRailWidth(width: number) {
  return Math.min(maximumRailWidth, Math.max(minimumRailWidth, width))
}

export function defaultRailPreferences(fleetWidth = defaultFleetRailWidth): RailPreferences {
  return {
    fleet: { width: clampRailWidth(fleetWidth), collapsed: false },
    notes: { width: defaultNotesRailWidth, collapsed: false },
  }
}

export function resizedRailWidth(width: number, side: RailSide, pointerDelta: number) {
  return clampRailWidth(width + (side === 'left' ? pointerDelta : -pointerDelta))
}

export function railWidthFromKey(width: number, side: RailSide, key: 'ArrowLeft' | 'ArrowRight') {
  const screenDelta = key === 'ArrowRight' ? 10 : -10
  return resizedRailWidth(width, side, screenDelta)
}
