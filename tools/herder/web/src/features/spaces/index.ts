export { SpaceStrip } from './SpaceStrip.tsx'
export { moveBeforeActiveClose, performSpaceSwitch, restoreSpaceDock } from './spacesControllerModel.ts'
export { createSpacesStore, defaultMaxSpaces, type SpacesStatus, type SpacesStore } from './spacesStore.ts'
export {
  closeSpaceLayout,
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
