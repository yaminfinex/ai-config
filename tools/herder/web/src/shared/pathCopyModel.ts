export type ClipboardWriter = { writeText: (value: string) => Promise<void> }
export type CopyPathState = 'copied' | 'failed'

export async function copyPath(clipboard: ClipboardWriter | undefined, value: string): Promise<CopyPathState> {
  if (!clipboard) return 'failed'
  try {
    await clipboard.writeText(value)
    return 'copied'
  } catch {
    return 'failed'
  }
}
