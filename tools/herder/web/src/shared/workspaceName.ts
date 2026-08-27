export function workspaceName(label: string, id: string) {
  return (label || id).replace(/-[0-9a-f]{8}$/i, '')
}
