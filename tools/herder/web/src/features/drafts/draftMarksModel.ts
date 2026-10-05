// Draft marks: what the owner has written for an agent but not sent. Both
// sources are browser-local: the composer draft (localStorage, by agent)
// and the notes filed under the agent's own group (the group its notes
// strip adds to and hands off from). 'general' is the unassigned group and
// belongs to no agent.
export type AgentDrafts = { message: boolean, notes: number }
export type DraftMarks = Readonly<Record<string, AgentDrafts>>

export const unassignedNotesGroup = 'general'
export const noDraftMarks: DraftMarks = {}

// draftMarks names only the agents with something unsent: a composer draft
// with non-blank text, or at least one note in the agent's group.
export function draftMarks(composer: Readonly<Record<string, string>>, notes: readonly { group: string }[]): DraftMarks {
  const marks: Record<string, AgentDrafts> = {}
  for (const [agent, text] of Object.entries(composer)) {
    if (text.trim()) marks[agent] = { message: true, notes: 0 }
  }
  for (const { group } of notes) {
    if (!group || group === unassignedNotesGroup) continue
    const current = marks[group] ?? { message: false, notes: 0 }
    marks[group] = { ...current, notes: current.notes + 1 }
  }
  return marks
}

export function sameDraftMarks(left: DraftMarks, right: DraftMarks): boolean {
  const names = Object.keys(left)
  if (names.length !== Object.keys(right).length) return false
  return names.every((name) => right[name] !== undefined && left[name].message === right[name].message && left[name].notes === right[name].notes)
}

// draftTitle names the kinds an agent's mark stands for.
export function draftTitle(drafts: AgentDrafts): string {
  const parts: string[] = []
  if (drafts.message) parts.push('unsent message')
  if (drafts.notes > 0) parts.push(`${drafts.notes} unsent note${drafts.notes === 1 ? '' : 's'}`)
  return parts.join(', ')
}

// draftCountLabel is a space's roll-up: how many of its agents have drafts.
export function draftCountLabel(count: number): string {
  return `${count} draft${count === 1 ? '' : 's'}`
}
