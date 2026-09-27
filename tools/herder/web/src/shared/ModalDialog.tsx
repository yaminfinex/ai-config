import type { MouseEvent, ReactNode, Ref } from 'react'
import { createPortal } from 'react-dom'

// ModalDialog is a native <dialog> opened with showModal(): the browser owns
// the focus trap, the inert background, Escape, and returning focus to the
// opener. The owner unmounts it from onClose; call dialog.close() to close it.
export function ModalDialog({ className, labelledBy, dialogRef, onClose, children }: {
  className: string
  labelledBy: string
  dialogRef?: Ref<HTMLDialogElement>
  onClose: () => void
  children: ReactNode
}) {
  return createPortal(<dialog className={`modal-dialog ${className}`} aria-labelledby={labelledBy} onClose={onClose} onClick={closeOnBackdrop}
    ref={(node) => {
      if (typeof dialogRef === 'function') dialogRef(node)
      else if (dialogRef) dialogRef.current = node
      if (!node || node.open) return
      node.showModal()
      node.querySelector<HTMLElement>('input, select, textarea')?.focus()
    }}>
    {children}
  </dialog>, document.body)
}

// A click on the ::backdrop targets the dialog itself outside its box.
function closeOnBackdrop(event: MouseEvent<HTMLDialogElement>) {
  const dialog = event.currentTarget
  if (event.target !== dialog) return
  const box = dialog.getBoundingClientRect()
  const inside = event.clientX >= box.left && event.clientX <= box.right && event.clientY >= box.top && event.clientY <= box.bottom
  if (!inside) dialog.close()
}
