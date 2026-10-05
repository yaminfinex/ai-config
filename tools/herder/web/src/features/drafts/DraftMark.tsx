import { draftTitle, type AgentDrafts } from './draftMarksModel.ts'

// DraftMark is the quiet outlined chip for an agent with something unsent,
// unlike the filled unread and blocked marks.
export function DraftMark({ drafts }: { drafts: AgentDrafts | undefined }) {
  if (!drafts) return null
  const title = draftTitle(drafts)
  return <span className="draft-mark" role="img" aria-label={title} title={title}>draft</span>
}
