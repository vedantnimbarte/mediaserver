import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'

export interface MenuAction {
  label: string
  icon?: ReactNode
  onSelect: () => void
  /** Renders in red and is separated from the rest. */
  destructive?: boolean
  disabled?: boolean
}

interface ContextMenuProps {
  x: number
  y: number
  actions: MenuAction[]
  onClose: () => void
}

/**
 * ContextMenu renders at a viewport position and flips itself when it would run off
 * an edge, which happens constantly for posters in the last column or bottom row.
 */
export function ContextMenu({ x, y, actions, onClose }: ContextMenuProps) {
  const ref = useRef<HTMLDivElement>(null)
  const [position, setPosition] = useState({ left: x, top: y })

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return

    const rect = el.getBoundingClientRect()
    const margin = 8

    let left = x
    let top = y
    if (left + rect.width > window.innerWidth - margin) left = window.innerWidth - rect.width - margin
    if (top + rect.height > window.innerHeight - margin) top = window.innerHeight - rect.height - margin

    setPosition({ left: Math.max(margin, left), top: Math.max(margin, top) })
  }, [x, y])

  useEffect(() => {
    function onPointerDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }

    // Capture phase so the menu closes even when the click lands on something that
    // stops propagation.
    document.addEventListener('mousedown', onPointerDown, true)
    window.addEventListener('keydown', onKey)
    window.addEventListener('resize', onClose)
    // A scroll under an open menu would leave it detached from its anchor.
    window.addEventListener('scroll', onClose, true)

    return () => {
      document.removeEventListener('mousedown', onPointerDown, true)
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('resize', onClose)
      window.removeEventListener('scroll', onClose, true)
    }
  }, [onClose])

  const regular = actions.filter((a) => !a.destructive)
  const destructive = actions.filter((a) => a.destructive)

  return (
    <div
      ref={ref}
      role="menu"
      style={{ left: position.left, top: position.top }}
      className="fixed z-modal min-w-52 animate-scale-in overflow-hidden rounded-lg border
                 border-ink-700 bg-ink-850 py-1 shadow-menu"
    >
      {regular.map((action) => (
        <MenuRow key={action.label} action={action} onClose={onClose} />
      ))}

      {destructive.length > 0 && regular.length > 0 && <div className="my-1 h-px bg-ink-700" />}

      {destructive.map((action) => (
        <MenuRow key={action.label} action={action} onClose={onClose} />
      ))}
    </div>
  )
}

function MenuRow({ action, onClose }: { action: MenuAction; onClose: () => void }) {
  return (
    <button
      role="menuitem"
      disabled={action.disabled}
      onClick={() => {
        action.onSelect()
        onClose()
      }}
      className={`flex w-full items-center gap-3 px-3 py-2 text-left text-sm transition-colors
                  disabled:cursor-not-allowed disabled:opacity-40 ${
                    action.destructive
                      ? 'text-red-300 hover:bg-red-950/50 hover:text-red-200'
                      : 'text-ink-200 hover:bg-ink-700 hover:text-white'
                  }`}
    >
      {action.icon && <span className="shrink-0 opacity-80">{action.icon}</span>}
      {action.label}
    </button>
  )
}

/**
 * useContextMenu wires right-click (and a long-press on touch) to a menu, returning
 * the props to spread onto the target and the element to render.
 */
export function useContextMenu(actions: MenuAction[]) {
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null)
  const longPress = useRef<number | null>(null)

  function open(e: React.MouseEvent) {
    e.preventDefault()
    e.stopPropagation()
    setAnchor({ x: e.clientX, y: e.clientY })
  }

  const handlers = {
    onContextMenu: open,
    // Touch devices have no right-click, so a half-second press opens the same menu.
    onTouchStart: (e: React.TouchEvent) => {
      const touch = e.touches[0]
      longPress.current = window.setTimeout(() => setAnchor({ x: touch.clientX, y: touch.clientY }), 500)
    },
    onTouchEnd: () => {
      if (longPress.current) window.clearTimeout(longPress.current)
    },
    onTouchMove: () => {
      if (longPress.current) window.clearTimeout(longPress.current)
    },
  }

  const menu = anchor ? (
    <ContextMenu x={anchor.x} y={anchor.y} actions={actions} onClose={() => setAnchor(null)} />
  ) : null

  return { handlers, menu, open: () => setAnchor({ x: 0, y: 0 }) }
}
