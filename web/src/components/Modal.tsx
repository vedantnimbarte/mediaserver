import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { AlertTriangle, X } from 'lucide-react'

/** Modal is the base dialog: a scrim, an escape hatch, and focus handling. */
export function Modal({
  open,
  onClose,
  title,
  children,
  footer,
  size = 'md',
}: {
  open: boolean
  onClose: () => void
  title?: string
  children: ReactNode
  footer?: ReactNode
  size?: 'sm' | 'md' | 'lg'
}) {
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return

    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)

    // Stop the page behind the dialog scrolling under the scrim.
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'

    // Move focus into the dialog so keyboard and screen-reader users are not left
    // behind on the page underneath.
    panelRef.current?.focus()

    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = previousOverflow
    }
  }, [open, onClose])

  if (!open) return null

  const widths = { sm: 'max-w-sm', md: 'max-w-lg', lg: 'max-w-3xl' }

  return (
    <div className="fixed inset-0 z-modal flex items-center justify-center p-4">
      <div className="absolute inset-0 animate-fade-in bg-black/75 backdrop-blur-sm" onClick={onClose} />

      <div
        ref={panelRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={`relative w-full ${widths[size]} animate-scale-in rounded-lg border border-ink-700
                    bg-ink-850 shadow-menu focus:outline-none`}
      >
        {title && (
          <div className="flex items-center justify-between border-b border-ink-700 px-5 py-4">
            <h2 className="text-base font-semibold text-white">{title}</h2>
            <button
              onClick={onClose}
              className="rounded p-1 text-ink-400 transition-colors hover:text-white"
              aria-label="Close"
            >
              <X className="h-5 w-5" />
            </button>
          </div>
        )}

        <div className="max-h-[70vh] overflow-y-auto px-5 py-4">{children}</div>

        {footer && <div className="flex justify-end gap-2 border-t border-ink-700 px-5 py-4">{footer}</div>}
      </div>
    </div>
  )
}

// ---- confirm() replacement ----

interface ConfirmOptions {
  title: string
  message?: string
  confirmLabel?: string
  cancelLabel?: string
  destructive?: boolean
}

interface ConfirmState {
  confirm: (options: ConfirmOptions) => Promise<boolean>
}

const ConfirmContext = createContext<ConfirmState | null>(null)

/**
 * ConfirmProvider replaces window.confirm, which cannot be styled, blocks the whole
 * browser tab, and looks like a phishing prompt in a full-screen media app.
 */
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<
    (ConfirmOptions & { resolve: (value: boolean) => void }) | null
  >(null)

  const confirm = useCallback((options: ConfirmOptions) => {
    return new Promise<boolean>((resolve) => setState({ ...options, resolve }))
  }, [])

  function settle(value: boolean) {
    state?.resolve(value)
    setState(null)
  }

  return (
    <ConfirmContext.Provider value={{ confirm }}>
      {children}

      <Modal
        open={state !== null}
        onClose={() => settle(false)}
        size="sm"
        footer={
          <>
            <button className="btn-outline" onClick={() => settle(false)}>
              {state?.cancelLabel ?? 'Cancel'}
            </button>
            <button
              className={state?.destructive ? 'btn-danger' : 'btn-primary'}
              onClick={() => settle(true)}
              autoFocus
            >
              {state?.confirmLabel ?? 'Confirm'}
            </button>
          </>
        }
      >
        <div className="flex gap-4">
          {state?.destructive && (
            <div className="shrink-0">
              <AlertTriangle className="h-6 w-6 text-amber-400" />
            </div>
          )}
          <div>
            <h2 className="text-base font-semibold text-white">{state?.title}</h2>
            {state?.message && <p className="mt-2 text-sm leading-relaxed text-ink-300">{state.message}</p>}
          </div>
        </div>
      </Modal>
    </ConfirmContext.Provider>
  )
}

export function useConfirm(): ConfirmState['confirm'] {
  const ctx = useContext(ConfirmContext)
  if (!ctx) throw new Error('useConfirm must be used inside a ConfirmProvider')
  return ctx.confirm
}
