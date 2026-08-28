import type { FileCandidate, ResolveResponse } from '../../types'
import { rootLabel } from './fileResolution'

export function RootOutcomes({ resolution }: { resolution?: ResolveResponse }) {
  const incomplete = resolution?.roots.filter((root) => root.status !== 'complete') ?? []
  if (incomplete.length === 0) return null
  return <div className="root-outcomes" role="status">{incomplete.map((root) => <div className={`root-outcome ${root.status}`} key={root.root}>
    <strong>{rootLabel(root.root)}</strong><span>{root.status}</span>{root.detail && <span title={root.detail}>{root.detail}</span>}
  </div>)}</div>
}

export function FileResults({ resolution, activeIndex = -1, onSelect, empty = 'No current matches.', limit = 100 }: {
  resolution?: ResolveResponse
  activeIndex?: number
  onSelect: (candidate: FileCandidate) => void
  empty?: string
  limit?: number
}) {
  if (!resolution) return null
  const visible = resolution.candidates.slice(0, limit)
  return <><RootOutcomes resolution={resolution} />
    {resolution.candidates.length === 0 ? <p className="file-results-empty">{empty}</p> : <div className="file-results" role="listbox" aria-label="Resolved files and folders">
      {visible.map((candidate, index) => <button type="button" role="option" aria-selected={index === activeIndex} className={index === activeIndex ? 'active' : ''}
        key={`${candidate.root}\0${candidate.kind}\0${candidate.path}`} onMouseDown={(event) => event.preventDefault()} onClick={() => onSelect(candidate)}>
        <span className={`file-result-path ${candidate.kind}`}><span aria-hidden="true">{candidate.kind === 'dir' ? '▰' : '◇'}</span>{candidate.path}</span>
        <span className="root-tag" title={candidate.root}>{rootLabel(candidate.root)}</span>
        <span className={`tier-tag ${candidate.tier}`}>{candidate.kind === 'dir' ? 'folder · ' : ''}{candidate.tier}</span>
      </button>)}
      {visible.length < resolution.candidates.length && <p className="file-results-limit">Showing {visible.length} of {resolution.candidates.length} ranked matches. Refine the query for more.</p>}
    </div>}
  </>
}
