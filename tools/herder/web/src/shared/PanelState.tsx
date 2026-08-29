import { useEffect, useRef, type ReactNode } from 'react'
export { failureBanner } from './panelState.ts'

export function useActivationRefetch(active: boolean, refetch: () => void) {
  const wasActive = useRef(active)
  const refetchRef = useRef(refetch)
  refetchRef.current = refetch
  useEffect(() => {
    if (active && !wasActive.current) refetchRef.current()
    wasActive.current = active
  }, [active])
}

export function PanelState({ as: Element = 'section', className, role = 'status', title, detail, children }: {
  as?: 'div' | 'main' | 'section'
  className?: string
  role?: 'alert' | 'status'
  title?: ReactNode
  detail?: ReactNode
  children?: ReactNode
}) {
  return <Element className={className} role={role}>{title !== undefined && <strong>{title}</strong>}{detail !== undefined && <p>{detail}</p>}{children}</Element>
}
