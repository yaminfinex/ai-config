import { useState } from 'react'
import { Dialog } from '@base-ui/react/dialog'
import { useMutation } from '@tanstack/react-query'
import { lifecycleProblem, spawnAgent, type SpawnRequest } from '../../api/client'
import type { Pane } from '../../types'

function placementNotice(action: string, result: { name: string, pane: string }) {
  return `${action} ${result.name} · ${result.pane || 'placement pending'}`
}

export function SpawnControl({ pane, onBanner }: { pane: Pane, onBanner: (key: string, detail: string) => void }) {
  const [open, setOpen] = useState(false)
  const [shape, setShape] = useState<SpawnRequest['shape']>('pane')
  const [tool, setTool] = useState<SpawnRequest['tool']>('codex')
  const [tag, setTag] = useState('')
  const [prompt, setPrompt] = useState('')
  const [branch, setBranch] = useState('')
  const [inlineProblem, setInlineProblem] = useState('')
  const [readOnly, setReadOnly] = useState('')
  const [notice, setNotice] = useState('')
  const problemKey = `spawn ${pane.pane_id}`
  const mutation = useMutation({ mutationFn: (body: SpawnRequest) => spawnAgent(body) })

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (mutation.isPending || readOnly) return
    setInlineProblem('')
    setNotice('')
    onBanner(problemKey, '')
    const body: SpawnRequest = { from_pane: pane.pane_id, shape, tool, tag, prompt }
    if (shape === 'worktree') body.branch = branch
    try {
      const result = await mutation.mutateAsync(body)
      setNotice(placementNotice('Started', result))
    } catch (error: unknown) {
      const problem = lifecycleProblem(error)
      if (problem.readOnly) setReadOnly(problem.readOnly)
      if (problem.inline) setInlineProblem(problem.inline)
      if (problem.banner) onBanner(problemKey, problem.banner)
    }
  }

  return <Dialog.Root open={open} onOpenChange={setOpen} modal={false}>
    {!open && <Dialog.Trigger className="compact">Spawn</Dialog.Trigger>}
    {open && <form className="lifecycle-form spawn-form" onSubmit={(event) => void submit(event)}>
      <div className="lifecycle-heading"><strong>Spawn from {pane.pane_id}</strong><Dialog.Close className="compact" disabled={mutation.isPending}>Close</Dialog.Close></div>
      {readOnly && <div className="read-only" role="alert"><strong>Read-only</strong><span>{readOnly}</span></div>}
      <div className="lifecycle-fields">
        <label>Shape<select value={shape} disabled={mutation.isPending || Boolean(readOnly)} onChange={(event) => setShape(event.target.value as typeof shape)}><option value="pane">Same tab</option><option value="tab">Same workspace</option><option value="worktree">New worktree</option></select></label>
        <label>Tool<select value={tool} disabled={mutation.isPending || Boolean(readOnly)} onChange={(event) => setTool(event.target.value as typeof tool)}><option value="claude">Claude</option><option value="codex">Codex</option></select></label>
        <label>Tag<input value={tag} required disabled={mutation.isPending || Boolean(readOnly)} onChange={(event) => setTag(event.target.value)} /></label>
        {shape === 'worktree' && <label>Branch<input value={branch} required disabled={mutation.isPending || Boolean(readOnly)} onChange={(event) => setBranch(event.target.value)} /></label>}
      </div>
      <label>Prompt<textarea rows={3} value={prompt} required disabled={mutation.isPending || Boolean(readOnly)} onChange={(event) => setPrompt(event.target.value)} /></label>
      <div className="send-footer">
        <div>{inlineProblem && <p className="inline-error" role="alert">{inlineProblem}</p>}{notice && <p className="send-notice">{notice}</p>}</div>
        <button type="submit" disabled={mutation.isPending || Boolean(readOnly)}>{mutation.isPending ? 'Spawning… this can take up to 150s' : 'Spawn agent'}</button>
      </div>
    </form>}
  </Dialog.Root>
}
