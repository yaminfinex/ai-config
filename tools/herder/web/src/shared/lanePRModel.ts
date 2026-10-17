import type { Board, Lane, LanePR } from '../types.ts'

// prFacts are a PR's states worth a glance, worst first: what is done to it
// (merged, closed), then what blocks it (conflicts, checks, review, open
// threads), then draft. A linked PR not polled yet says only that.
export function prFacts(pr: LanePR): string[] {
  if (!pr.state) return ['not polled yet']
  if (pr.state === 'MERGED') return ['merged']
  if (pr.state === 'CLOSED') return ['closed']
  const facts: string[] = []
  if (pr.mergeable === 'CONFLICTING') facts.push('conflicts')
  if (pr.checks === 'FAILURE' || pr.checks === 'ERROR') facts.push('checks failing')
  else if (pr.checks === 'PENDING' || pr.checks === 'EXPECTED') facts.push('checks running')
  else if (pr.checks === 'SUCCESS') facts.push('checks pass')
  if (pr.review === 'CHANGES_REQUESTED') facts.push('changes requested')
  else if (pr.review === 'APPROVED') facts.push('approved')
  else if (pr.review === 'REVIEW_REQUIRED') facts.push('review needed')
  if (pr.unresolved > 0) facts.push(`${pr.unresolved} unresolved`)
  if (pr.draft) facts.push('draft')
  return facts
}

// prLabel is the row text: number and title.
export function prLabel(pr: LanePR) {
  return pr.title ? `#${pr.number} ${pr.title}` : `#${pr.number}`
}

// prSummary is one PR in a line: `#12 checks pass, approved`.
export function prSummary(pr: LanePR) {
  return `#${pr.number} ${prFacts(pr).join(', ')}`
}

// PRQuery is what the palette reads as a PR: `#123`, `owner/repo#123` or a
// github.com pull request URL. repo is lower case; absent for `#123`.
export type PRQuery = { repo?: string, number: number }

export function parsePRQuery(text: string): PRQuery | null {
  const query = text.trim()
  const bare = query.match(/^#(\d+)$/)
  if (bare) return { number: Number(bare[1]) }
  const short = query.match(/^([A-Za-z0-9-]+\/[A-Za-z0-9._-]+)#(\d+)$/)
  if (short) return { repo: short[1]!.toLowerCase(), number: Number(short[2]) }
  const link = query.match(/^https?:\/\/(?:www\.)?github\.com\/([A-Za-z0-9-]+\/[A-Za-z0-9._-]+)\/pull\/(\d+)(?:[/?#].*)?$/i)
  if (link) return { repo: link[1]!.toLowerCase(), number: Number(link[2]) }
  return null
}

// prLanes is every lane on the board holding the PR the query names, with
// that PR; `#123` matches the number in any repo.
export function prLanes(board: Board | undefined, query: PRQuery): { lane: Lane, pr: LanePR }[] {
  return (board?.lanes ?? []).flatMap((lane) => (lane.prs ?? [])
    .filter((pr) => pr.number === query.number && (!query.repo || pr.repo === query.repo))
    .map((pr) => ({ lane, pr })))
}
