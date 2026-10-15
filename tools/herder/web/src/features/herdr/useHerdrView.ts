import { useCallback, useEffect, useRef, useState } from 'react'
import { bindHerdrViewChord } from '../layout/shellShortcuts.ts'

// useHerdrView owns whether the herdr view is up: ⌃` (or the status bar's
// control) opens it from the lens or a zoom, and the same chord or its ×
// leaves it, handing focus back to where it was. mounted stays true once
// opened, so the view keeps its screen while away. showTerminal opens it on
// one pane (a sidebar terminal no agent owns); terminal is that pane until
// the view is left.
export function useHerdrView() {
  const [open, setOpen] = useState(false)
  const [mounted, setMounted] = useState(false)
  const [terminal, setTerminal] = useState<string>()
  const returnFocus = useRef<HTMLElement | null>(null)
  const show = useCallback(() => {
    const active = document.activeElement
    returnFocus.current = active instanceof HTMLElement && !active.closest('.herdr-view') ? active : null
    setMounted(true)
    setOpen(true)
  }, [])
  const showTerminal = useCallback((paneID: string) => {
    setTerminal(paneID)
    show()
  }, [show])
  const leave = useCallback(() => {
    setOpen(false)
    setTerminal(undefined)
    const back = returnFocus.current
    returnFocus.current = null
    // After the view hides, so focus does not land inside it.
    requestAnimationFrame(() => { if (back?.isConnected) back.focus() })
  }, [])
  const openRef = useRef(open)
  openRef.current = open
  const toggle = useCallback(() => { if (openRef.current) leave(); else show() }, [leave, show])
  useEffect(() => bindHerdrViewChord(window, toggle), [toggle])
  return { open, mounted, terminal, show, showTerminal, leave, toggle }
}
