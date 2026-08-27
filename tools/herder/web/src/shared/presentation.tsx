export function statusClass(status: string) {
  if (status === 'working' || status === 'active') return 'working'
  if (status === 'idle' || status === 'listening') return 'idle'
  if (status === 'dead') return 'dead'
  return 'unknown'
}

export function gapLabel(gap: string) {
  if (gap === '-') return ''
  return gap.toLowerCase().includes('pane') ? 'no pane' : 'gap'
}

export function workspaceName(label: string, id: string) {
  return (label || id).replace(/-[0-9a-f]{8}$/i, '')
}

export function Banner({ source, detail }: { source: string, detail: string }) {
  return <div className="banner" role="alert"><strong>{source}</strong><span>{detail}</span></div>
}
