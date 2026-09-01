export { SpaceStrip } from './SpaceStrip.tsx'
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
  type SpacesInitialization,
  type SpaceResult,
} from './spacesModel.ts'
