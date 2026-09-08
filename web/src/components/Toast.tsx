import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react'

type ToastKind = 'success' | 'error' | 'info' | 'warning'

interface Toast {
  id: number
  kind: ToastKind
  title: string
  message?: string
  /** Milliseconds before auto-dismiss; 0 keeps it until dismissed. */
  duration: number
}

interface ToastState {
  show: (toast: Omit<Toast, 'id' | 'duration'> & { duration?: number }) => void
  success: (title: string, message?: string) => void
  error: (title: string, message?: string) => void
  info: (title: string, message?: string) => void
}

const ToastContext = createContext<ToastState | null>(null)

let nextId = 1

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const dismiss = useCallback((id: number) => {
    setToasts((current) => current.filter((t) => t.id !== id))
  }, [])

  const show = useCallback<ToastState['show']>((toast) => {
    const id = nextId++
    // Errors stay until dismissed: they usually carry something the user needs to
    // read and act on, and four seconds is not enough for that.
    const duration = toast.duration ?? (toast.kind === 'error' ? 0 : 4000)
    setToasts((current) => [...current, { ...toast, id, duration }])
  }, [])

  const success = useCallback((title: string, message?: string) => show({ kind: 'success', title, message }), [show])
  const error = useCallback((title: string, message?: string) => show({ kind: 'error', title, message }), [show])
  const info = useCallback((title: string, message?: string) => show({ kind: 'info', title, message }), [show])

  return (
    <ToastContext.Provider value={{ show, success, error, info }}>
      {children}
      <div
        className="pointer-events-none fixed bottom-4 right-4 z-toast flex w-full max-w-sm flex-col gap-2"
        role="region"
        aria-label="Notifications"
      >
        {toasts.map((toast) => (
          <ToastCard key={toast.id} toast={toast} onDismiss={() => dismiss(toast.id)} />
        ))}
      </div>
    </ToastContext.Provider>
  )
}

const styles: Record<ToastKind, { icon: ReactNode; accent: string }> = {
  success: { icon: <CheckCircle2 className="h-5 w-5 text-emerald-400" />, accent: 'border-l-emerald-500' },
  error: { icon: <XCircle className="h-5 w-5 text-red-400" />, accent: 'border-l-red-500' },
  warning: { icon: <AlertTriangle className="h-5 w-5 text-amber-400" />, accent: 'border-l-amber-500' },
  info: { icon: <Info className="h-5 w-5 text-sky-400" />, accent: 'border-l-sky-500' },
}

function ToastCard({ toast, onDismiss }: { toast: Toast; onDismiss: () => void }) {
  useEffect(() => {
    if (toast.duration <= 0) return
    const timer = window.setTimeout(onDismiss, toast.duration)
    return () => window.clearTimeout(timer)
  }, [toast.duration, onDismiss])

  const style = styles[toast.kind]

  return (
    <div
      className={`pointer-events-auto flex animate-slide-up gap-3 rounded-lg border border-ink-700 ${style.accent}
                  border-l-4 bg-ink-850 p-4 shadow-menu`}
      // Errors interrupt; everything else is announced politely so a screen reader
      // is not yanked away mid-sentence by a "saved" confirmation.
      role={toast.kind === 'error' ? 'alert' : 'status'}
    >
      <div className="shrink-0">{style.icon}</div>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-white">{toast.title}</p>
        {toast.message && <p className="mt-0.5 text-xs leading-relaxed text-ink-300">{toast.message}</p>}
      </div>
      <button
        onClick={onDismiss}
        className="shrink-0 rounded p-0.5 text-ink-400 transition-colors hover:text-white"
        aria-label="Dismiss"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  )
}

export function useToast(): ToastState {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used inside a ToastProvider')
  return ctx
}
