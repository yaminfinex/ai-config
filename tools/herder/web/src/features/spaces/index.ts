export { SpaceStrip } from './SpaceStrip.tsx'
export { createAndSwitchSpace, moveBeforeActiveClose, performSpaceSwitch, restoreSpaceDock, sendPanelToExistingSpace, sendPanelToNewSpace, spaceIDInDirection } from './spacesControllerModel.ts'
export { createSpacesStore, defaultMaxSpaces, type SpacesStatus, type SpacesStore } from './spacesStore.ts'
export { browserSpacesTransport, createServerSpaceLookup, createSpacesSync, createSpacesSyncPersistence, resetSpacesSyncCursor, serverSpaceLookupMessage, spacesStoreSyncAdapter } from './spacesSync.ts'
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
