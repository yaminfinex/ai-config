export type ClipboardWriter = { writeText: (value: string) => Promise<void> }
export type LegacyClipboardWriter = (value: string) => boolean
export type CopyPathState = 'copied' | 'failed'

export async function copyPath(
  clipboard: ClipboardWriter | undefined,
  value: string,
  legacyWrite?: LegacyClipboardWriter,
): Promise<CopyPathState> {
  if (clipboard) {
    try {
      await clipboard.writeText(value)
      return 'copied'
    } catch {
      // Fall through to the user-activation-compatible legacy path.
    }
  }
  try {
    return legacyWrite?.(value) ? 'copied' : 'failed'
  } catch {
    return 'failed'
  }
}
