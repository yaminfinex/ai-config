import { useState, type RefObject } from 'react'
import { validateHostAlias } from './vscodeModel.ts'

export const hostAliasHint = "The Host name in your laptop's ~/.ssh/config for this machine."

// HostAliasForm validates on submit and shows the refusal inline; it never
// stores anything itself. It stays mounted across saves so focus stays put,
// and takes a new initial value (a save, clear, or another tab) in place.
export function HostAliasForm({ hostKey, initial, submitLabel, inputRef, onSave, onCancel }: {
  hostKey: string
  initial: string
  submitLabel: string
  inputRef?: RefObject<HTMLInputElement | null>
  onSave: (alias: string) => void
  onCancel?: () => void
}) {
  const [value, setValue] = useState(initial)
  const [problem, setProblem] = useState('')
  const [seen, setSeen] = useState(initial)
  if (seen !== initial) { setSeen(initial); setValue(initial); setProblem('') }
  const inputID = `vscode-alias-${hostKey}`
  return <form className="vscode-alias-form" noValidate onSubmit={(event) => {
    event.preventDefault()
    const alias = validateHostAlias(value)
    if (!alias.ok) { setProblem(alias.reason); return }
    setProblem('')
    onSave(alias.value)
  }}>
    <label htmlFor={inputID}>SSH host for <strong>{hostKey}</strong></label>
    <input id={inputID} ref={inputRef} value={value} spellCheck={false} autoCapitalize="off" autoComplete="off" placeholder="e.g. devbox"
      aria-invalid={Boolean(problem) || undefined} aria-describedby={`${inputID}-hint${problem ? ` ${inputID}-problem` : ''}`}
      onChange={(event) => { setValue(event.target.value); setProblem('') }} />
    <p id={`${inputID}-hint`} className="vscode-alias-hint">{hostAliasHint}</p>
    {problem && <p id={`${inputID}-problem`} className="vscode-alias-problem" role="alert">{problem}</p>}
    <div className="launch-agent-actions">{onCancel && <button type="button" onClick={onCancel}>Cancel</button>}<button type="submit">{submitLabel}</button></div>
  </form>
}
