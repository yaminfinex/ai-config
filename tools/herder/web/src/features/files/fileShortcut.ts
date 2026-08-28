export function isQuickOpenShortcut(event: Pick<KeyboardEvent, 'key' | 'ctrlKey' | 'metaKey'>, userAgent: string) {
  const command = event.ctrlKey || event.metaKey
  return command && event.key.toLowerCase() === 'k' || userAgent.includes('Mac') && event.metaKey && event.key === '/'
}
