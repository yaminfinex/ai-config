import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiProblem, queryKeys, sendMessage, viewerReadOnlyMessage } from '../../api/client'
import { blurComposerOnEscape, composerFieldId, isComposerQueueShortcut, isComposerSendShortcut, persistComposerDraft, readComposerDraft, resizeComposerFromMirror, subscribeComposerDraft } from '../../composerState'
import { beginSendRefresh, settleSendRefresh } from '../../sendRefresh'

export function Composer({ name, identityReadOnly, onViewer, onProblem, onSend, onQueue }: {
  name: string
  identityReadOnly: string
  onViewer: (viewer: string) => void
  onProblem: (detail: string) => void
  onSend: () => void
  onQueue: (text: string) => { ok: true } | { ok: false, reason: string }
}) {
  const [message, setMessage] = useState(() => readComposerDraft(name))
  const [sendProblem, setSendProblem] = useState('')
  const [readOnly, setReadOnly] = useState('')
  const queryClient = useQueryClient()
  const composerRef = useRef<HTMLTextAreaElement>(null)
  const measureRef = useRef<HTMLTextAreaElement>(null)
  const mutation = useMutation({ mutationFn: (text: string) => sendMessage(name, text) })
  const effectiveReadOnly = identityReadOnly || readOnly
  const fieldId = composerFieldId(name)

  useEffect(() => subscribeComposerDraft(name, setMessage), [name])

  useLayoutEffect(() => {
    persistComposerDraft(name, message)
    const composer = composerRef.current
    const mirror = measureRef.current
    if (!composer || !mirror) return
    resizeComposerFromMirror(composer, mirror)
  }, [message, name])

  const send = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!message.trim() || mutation.isPending || effectiveReadOnly) return
    onSend()
    setSendProblem('')
    const sendRefresh = beginSendRefresh(queryClient, name)
    const refresh = () => Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.agent(name), exact: true }),
      queryClient.invalidateQueries({ queryKey: queryKeys.entries(name), exact: true }),
    ])
    try {
      const result = await mutation.mutateAsync(message)
      onViewer(result.from)
      persistComposerDraft(name, '')
      setMessage('')
      await settleSendRefresh(sendRefresh, true, refresh)
      onProblem('')
    } catch (error: unknown) {
      await settleSendRefresh(sendRefresh, false, refresh)
      const { response, problem } = apiProblem(error)
      if (response?.status === 409 && (problem.error === 'attribution required' || problem.error === 'sender refused')) setReadOnly(viewerReadOnlyMessage(problem, response.status))
      else if (response?.status === 502) onProblem(problem.detail)
      else setSendProblem(`${problem.error}: ${problem.detail}`)
    }
  }

  return <form className="send-box" onSubmit={(event) => void send(event)}>
    {effectiveReadOnly && <div className="read-only" role="alert"><strong>Read-only</strong><span>{effectiveReadOnly}</span></div>}
    <textarea
      id={fieldId}
      data-composer
      aria-label={`Message ${name}`}
      ref={composerRef}
      rows={1}
      value={message}
      disabled={Boolean(effectiveReadOnly) || mutation.isPending}
      onChange={(event) => setMessage(event.target.value)}
      onKeyDown={(event) => {
        if (blurComposerOnEscape(event)) return
        if (isComposerQueueShortcut(event) && !event.nativeEvent.isComposing) {
          event.preventDefault()
          if (!message.trim()) return
          const queued = onQueue(message)
          if (!queued.ok) { setSendProblem(queued.reason); return }
          persistComposerDraft(name, '')
          setMessage('')
          setSendProblem('')
          return
        }
        if (!isComposerSendShortcut(event) || event.nativeEvent.isComposing) return
        event.preventDefault()
        event.currentTarget.form?.requestSubmit()
      }}
      placeholder="Send an attributed request…"
    />
    <textarea
      aria-hidden="true"
      className="composer-measure"
      inert
      readOnly
      ref={measureRef}
      tabIndex={-1}
      value={message}
    />
    <div className="send-footer">
      <div>{sendProblem && <p className="inline-error" role="alert">{sendProblem}</p>}</div>
      <span className="composer-key-hints"><span><kbd>⌘⏎</kbd> send</span><span><kbd>⌥⏎</kbd> queue as note</span></span>
      <button type="submit" disabled={!message.trim() || mutation.isPending || Boolean(effectiveReadOnly)}>{mutation.isPending ? 'Sending…' : 'Send request'}</button>
    </div>
  </form>
}
