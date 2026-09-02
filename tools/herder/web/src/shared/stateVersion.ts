export function compareStateVersions(leftUpdated: number, leftWriteID: string, rightUpdated: number, rightWriteID: string) {
  if (leftUpdated !== rightUpdated) return leftUpdated > rightUpdated ? 1 : -1
  if (leftWriteID === rightWriteID) return 0
  return leftWriteID > rightWriteID ? 1 : -1
}
