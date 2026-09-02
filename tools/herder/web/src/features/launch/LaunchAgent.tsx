import { useEffect, useRef, useState } from 'react'
import { apiProblem, spawnAgent } from '../../api/client.ts'
import { changeLaunchTool, initialLaunchForm, launchConfirmation, launchRefusal, launchRequest, type LaunchTool } from './launchModel.ts'

export function LaunchAgent({ onOpenAgent }: { onOpenAgent: (name: string) => void }) {
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState(initialLaunchForm)
  const [pending, setPending] = useState(false)
  const [problem, setProblem] = useState('')
  const [result, setResult] = useState<{ names: string[], output_tail: string } | null>(null)
  const tool = useRef<HTMLSelectElement | null>(null)

  useEffect(() => { if (open) tool.current?.focus() }, [open])

  const close = () => {
    setOpen(false)
    setProblem('')
    setResult(null)
  }
  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    setPending(true)
    setProblem('')
    setResult(null)
    try {
      setResult(await spawnAgent(launchRequest(form)))
    } catch (error) {
      setProblem(launchRefusal(apiProblem(error).problem))
    } finally {
      setPending(false)
    }
  }
  const confirmation = result ? launchConfirmation(result.names) : null

  return <>
    <button type="button" className="launch-agent-button" aria-label="Launch agent" title="Launch agent" onClick={() => setOpen(true)}>+</button>
    {open && <div className="launch-agent-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) close() }}>
      <section className="launch-agent-dialog" role="dialog" aria-modal="true" aria-labelledby="launch-agent-title" onKeyDown={(event) => {
        if (event.key === 'Escape') { event.preventDefault(); close() }
      }}>
        <header><strong id="launch-agent-title">Launch agent</strong><button type="button" aria-label="Close launch form" onClick={close}>×</button></header>
        <form onSubmit={submit}>
          <label>Tool<select ref={tool} value={form.tool} disabled={pending} onChange={(event) => setForm((current) => changeLaunchTool(current, event.target.value as LaunchTool))}>
            <option value="claude">Claude</option><option value="codex">Codex</option>
          </select></label>
          <label>Model<input list={`launch-${form.tool}-models`} value={form.model} disabled={pending} maxLength={120} placeholder="default"
            onChange={(event) => setForm((current) => ({ ...current, model: event.target.value }))} /></label>
          <datalist id={`launch-${form.tool}-models`}>{form.modelOptions.map((model) => <option value={model} key={model} />)}</datalist>
          <label>Tag<input value={form.tag} disabled={pending} pattern="[A-Za-z0-9][A-Za-z0-9_-]*" required
            onChange={(event) => setForm((current) => ({ ...current, tag: event.target.value }))} /></label>
          <label>Repository<input value={form.repo} disabled={pending} placeholder="This Herder repo"
            onChange={(event) => setForm((current) => ({ ...current, repo: event.target.value }))} /></label>
          <label>Worktree branch<input value={form.branch} disabled={pending} placeholder="Generated automatically"
            onChange={(event) => setForm((current) => ({ ...current, branch: event.target.value }))} /></label>
          <p className="launch-agent-help">{form.branchHelp}</p>
          <div className="launch-agent-actions"><button type="button" onClick={close}>Cancel</button><button type="submit" disabled={pending}>{pending ? 'Launching…' : 'Launch'}</button></div>
        </form>
        {problem && <pre className="launch-agent-refusal" role="alert">{problem}</pre>}
        {confirmation && <div className="launch-agent-confirmation" role="status" aria-live="polite">
          <span>{confirmation.line}</span>
          {confirmation.action && <button type="button" onClick={() => onOpenAgent(confirmation.action!.agent)}>{confirmation.action.label}</button>}
          {result?.output_tail && <details><summary>Launch output</summary><pre>{result.output_tail}</pre></details>}
        </div>}
      </section>
    </div>}
  </>
}
