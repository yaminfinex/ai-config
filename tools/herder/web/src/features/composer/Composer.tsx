import { useLayoutEffect, useRef, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { apiProblem, sendMessage } from '../../api/client'
import { isComposerSendShortcut, persistComposerDraft, readComposerDraft } from '../../composerState'

export function Composer({ name, identityReadOnly, onViewer, onProblem }: {
  name: string
  identityReadOnly: string
  onViewer: (viewer: string) => void
  onProblem: (detail: string) => void
}) {
  const [message, setMessage] = useState(() => readComposerDraft(name))
  const [sendProblem, setSendProblem] = useState('')
  const [sendNotice, setSendNotice] = useState('')
  const [readOnly, setReadOnly] = useState('')
  const composerRef = useRef<HTMLTextAreaElement>(null)
  const mutation = useMutation({ mutationFn: (text: string) => sendMessage(name, text) })
  const effectiveReadOnly = identityReadOnly || readOnly

  useLayoutEffect(() => {
    persistComposerDraft(name, message)
    const composer = composerRef.current
    if (!composer) return
    composer.style.height = '0px'
    const height = Math.min(composer.scrollHeight, 160)
    composer.style.height = `${height}px`
    composer.style.overflowY = composer.scrollHeight > 160 ? 'auto' : 'hidden'
  }, [message, name])

  const send = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!message.trim() || mutation.isPending || effectiveReadOnly) return
    setSendProblem('')
    setSendNotice('')
    try {
      const result = await mutation.mutateAsync(message)
      onViewer(result.from)
      persistComposerDraft(name, '')
      setMessage('')
      setSendNotice(`Sent to ${result.to} as ${result.from}. Waiting for the live reply…`)
      onProblem('')
    } catch (error: unknown) {
      const { response, problem } = apiProblem(error)
      if (response?.status === 409 && problem.error === 'attribution required') setReadOnly(`Connect via Tailscale to send. ${problem.detail}`)
      else if (response?.status === 502) onProblem(problem.detail)
      else setSendProblem(`${problem.error}: ${problem.detail}`)
    }
  }

  return <form className="send-box" onSubmit={(event) => void send(event)}>
    {effectiveReadOnly && <div className="read-only" role="alert"><strong>Read-only</strong><span>{effectiveReadOnly}</span></div>}
    <label htmlFor="message">Message {name} <span>· Enter for newline · Ctrl/Cmd+Enter to send</span></label><textarea
      id="message"
      ref={composerRef}
      rows={1}
      value={message}
      disabled={Boolean(effectiveReadOnly) || mutation.isPending}
      onChange={(event) => setMessage(event.target.value)}
      onKeyDown={(event) => {
        if (!isComposerSendShortcut(event) || event.nativeEvent.isComposing) return
        event.preventDefault()
        event.currentTarget.form?.requestSubmit()
      }}
      placeholder="Send an attributed request…"
    />
    <div className="send-footer">
      <div>{sendProblem && <p className="inline-error" role="alert">{sendProblem}</p>}{sendNotice && <p className="send-notice">{sendNotice}</p>}</div>
      <button type="submit" disabled={!message.trim() || mutation.isPending || Boolean(effectiveReadOnly)}>{mutation.isPending ? 'Sending…' : 'Send request'}</button>
    </div>
    <p className="attribution-copy">sends as an attributed web viewer · web senders are not addressable bus peers</p>
  </form>
}
