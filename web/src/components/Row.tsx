import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'

/**
 * Row is the horizontal carousel: a scrollable track with arrow scrubbers that appear
 * on hover and hide themselves at each end.
 *
 * The track scrolls by a whole viewport-width of cards rather than a fixed pixel
 * amount, so one click always advances by a clean "page" regardless of card size or
 * window width.
 */
export function Row({
  title,
  children,
  action,
}: {
  title: string
  children: ReactNode
  action?: ReactNode
}) {
  const trackRef = useRef<HTMLDivElement>(null)
  const [canScrollLeft, setCanScrollLeft] = useState(false)
  const [canScrollRight, setCanScrollRight] = useState(false)

  const updateArrows = useCallback(() => {
    const el = trackRef.current
    if (!el) return
    // The one-pixel tolerance absorbs sub-pixel rounding, which would otherwise leave
    // the right arrow visible forever on some zoom levels.
    setCanScrollLeft(el.scrollLeft > 1)
    setCanScrollRight(el.scrollLeft + el.clientWidth < el.scrollWidth - 1)
  }, [])

  useEffect(() => {
    const el = trackRef.current
    if (!el) return

    updateArrows()
    el.addEventListener('scroll', updateArrows, { passive: true })

    // Card images load after mount and change the scroll width, so observe the track
    // rather than measuring once.
    const observer = new ResizeObserver(updateArrows)
    observer.observe(el)

    return () => {
      el.removeEventListener('scroll', updateArrows)
      observer.disconnect()
    }
  }, [updateArrows, children])

  function scrollBy(direction: 1 | -1) {
    const el = trackRef.current
    if (!el) return
    el.scrollBy({ left: direction * (el.clientWidth * 0.85), behavior: 'smooth' })
  }

  return (
    <section className="group/row relative mb-6">
      <div className="mb-1 flex items-center gap-3 px-4 lg:px-12">
        <h2 className="text-lg font-semibold text-ink-200 lg:text-xl">{title}</h2>
        {action}
      </div>

      <div className="relative">
        {canScrollLeft && (
          <ScrubButton side="left" onClick={() => scrollBy(-1)} />
        )}

        <div ref={trackRef} className="row-track">
          {children}
        </div>

        {canScrollRight && (
          <ScrubButton side="right" onClick={() => scrollBy(1)} />
        )}
      </div>
    </section>
  )
}

function ScrubButton({ side, onClick }: { side: 'left' | 'right'; onClick: () => void }) {
  const Icon = side === 'left' ? ChevronLeft : ChevronRight
  return (
    <button
      onClick={onClick}
      aria-label={side === 'left' ? 'Scroll left' : 'Scroll right'}
      className={`absolute inset-y-8 z-20 flex w-10 items-center justify-center bg-ink-900/60
                  text-white opacity-0 transition-opacity duration-200 hover:bg-ink-900/85
                  focus-visible:opacity-100 group-hover/row:opacity-100 lg:w-12
                  ${side === 'left' ? 'left-0' : 'right-0'}`}
    >
      <Icon className="h-8 w-8" />
    </button>
  )
}
