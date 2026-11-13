import { useEffect, useRef, useState, type ReactNode } from 'react'
import { CheckCircle2, Info, X, XCircle } from 'lucide-react'
import { useUI } from '../../stores/ui'
import { Button, IconButton, cx } from './primitives'

/** Accessible modal: focus is trapped, Esc closes, backdrop click closes. */
export function Modal({
  title,
  onClose,
  children,
  footer,
  width = 'max-w-lg',
}: {
  title: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  width?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null
    const first = ref.current?.querySelector<HTMLElement>(
      'input, select, textarea, button:not([data-close])',
    )
    ;(first ?? ref.current)?.focus()
    return () => prev?.focus()
  }, [])
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation()
      onClose()
    }
    if (e.key === 'Tab' && ref.current) {
      const f = ref.current.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])',
      )
      const a = f[0]
      const z = f[f.length - 1]
      if (e.shiftKey && document.activeElement === a) {
        e.preventDefault()
        z?.focus()
      } else if (!e.shiftKey && document.activeElement === z) {
        e.preventDefault()
        a?.focus()
      }
    }
  }
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-6"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        onKeyDown={onKey}
        className={cx(
          'fade-in flex max-h-full w-full flex-col rounded-lg border border-line bg-surface shadow-xl',
          width,
        )}
      >
        <header className="flex items-center justify-between border-b border-line px-4 py-3">
          <h2 className="text-base font-semibold">{title}</h2>
          <IconButton label="Close" data-close onClick={onClose}>
            <X className="h-4 w-4" />
          </IconButton>
        </header>
        <div className="min-h-0 flex-1 overflow-auto p-4">{children}</div>
        {footer && (
          <footer className="flex justify-end gap-2 border-t border-line bg-subtle px-4 py-3">
            {footer}
          </footer>
        )}
      </div>
    </div>
  )
}

/** Global confirmation dialog driven by the UI store. */
export function ConfirmHost() {
  const req = useUI((s) => s.confirm)
  const close = useUI((s) => s.closeConfirm)
  const [busy, setBusy] = useState(false)
  if (!req) return null
  const run = async () => {
    setBusy(true)
    try {
      await req.onConfirm()
    } finally {
      setBusy(false)
      close()
    }
  }
  return (
    <Modal
      title={req.title}
      onClose={close}
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button variant={req.danger ? 'danger' : 'primary'} disabled={busy} onClick={run}>
            {req.confirmLabel}
          </Button>
        </>
      }
    >
      <div className="text-sm">{req.body}</div>
    </Modal>
  )
}

export function Toasts() {
  const toasts = useUI((s) => s.toasts)
  const dismiss = useUI((s) => s.dismissToast)
  return (
    <div
      aria-live="polite"
      className="pointer-events-none fixed right-4 bottom-4 z-[60] flex w-80 flex-col gap-2"
    >
      {toasts.map((t) => (
        <div
          key={t.id}
          role={t.kind === 'error' ? 'alert' : 'status'}
          className="fade-in pointer-events-auto flex gap-2 rounded-lg border border-line bg-surface p-3 shadow-lg"
        >
          {t.kind === 'success' ? (
            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-ok" />
          ) : t.kind === 'error' ? (
            <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-bad" />
          ) : (
            <Info className="mt-0.5 h-4 w-4 shrink-0 text-accent" />
          )}
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">{t.title}</div>
            {t.detail && (
              <div className="selectable break-words text-xs text-muted">{t.detail}</div>
            )}
          </div>
          <IconButton label="Dismiss" className="h-5 w-5" onClick={() => dismiss(t.id)}>
            <X className="h-3 w-3" />
          </IconButton>
        </div>
      ))}
    </div>
  )
}
