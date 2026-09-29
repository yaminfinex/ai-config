export { createAndSwitchSpace, moveBeforeActiveClose, performSpaceSwitch, restoreSpaceDock, sendPanelToExistingSpace, sendPanelToNewSpace, spaceIDInDirection } from './spacesControllerModel.ts'
export { createSpacesStore, defaultMaxSpaces, type SpacesStatus, type SpacesStore } from './spacesStore.ts'
export { browserOnlySpacesMessage, browserSpacesTransport, createServerSpaceLookup, createSpacesSync, createSpacesSyncPersistence, resetSpacesSyncCursor, serverSpaceLookupMessage, spacesStoreSyncAdapter } from './spacesSync.ts'
export {
  closeSpaceLayout,
  hasRecoverableSpaceLayout,
  initializeSpaces,
  readLegacyLayoutFamilies,
  readActiveSpace,
  removeLayoutRecovery,
  reopenSpaceLayout,
  writeActiveSpace,
  type SpaceDefinition,
  type LegacyLayoutFamilies,
  type SpacesInitialization,
  type SpaceResult,
} from './spacesModel.ts'
export { agentsInDock, attentionLabel, markViewedRead, pruneReadMarkers, quietAttention, seedReadMarkers, spaceAttention, storedSpaceAgents, totalAttention, turnEnd, type SpaceAttention } from './spaceAttentionModel.ts'
export { parseReadMarkers, readMarkersKey, readReadMarkers, writeReadMarkers, type ReadMarkers } from './readMarkerStore.ts'
export { mruSpaceIDs, readSpaceMRU, touchSpaceMRU, writeSpaceMRU } from './spaceMRU.ts'
export { dwelledAgents, nextDwellDelay, nextViewing, viewedAgents, viewDwellMs, type ViewingState } from './viewingModel.ts'
export { highlightedSpace, idleSwitcher, reduceSwitcher, type SwitcherEvent, type SwitcherState } from './spaceSwitcherModel.ts'
export { SpacesSection } from './SpacesSection.tsx'
export { SpaceSwitcher } from './SpaceSwitcher.tsx'
export { focusAtEnd, focusOrigin, switchFocusDecision, type SwitchFocusDecision, type SwitchFocusInput } from './switchFocusModel.ts'
