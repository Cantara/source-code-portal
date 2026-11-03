"use client"

import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type FocusEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
} from "react"
import { createPortal } from "react-dom"
import { cn } from "@/lib/cn"

/** The props a Hint hands its trigger: spread them on the element that carries the signal. */
export type HintTrigger = {
  ref: (el: HTMLElement | null) => void
  onPointerDown: (e: PointerEvent) => void
  onPointerEnter: (e: PointerEvent) => void
  onPointerLeave: (e: PointerEvent) => void
  onFocus: (e: FocusEvent) => void
  onBlur: (e: FocusEvent) => void
  onClick: (e: MouseEvent) => void
  "aria-describedby"?: string
}

/** HintRow is one signal of a Hint. */
export type HintRow = { swatch?: string; label: string; value?: string }

const opened = "scp-hint-open" // the event that closes every other hint: one shows at a time
const gap = 8 // px between the trigger and the card, and the card's least distance to the viewport's edges

/** tapped reports whether the element's last press was a finger's or a pen's. */
function tapped(el: EventTarget): boolean {
  return el instanceof HTMLElement && el.dataset.touch === "true"
}

/**
 * Hint names a signal (a coloured tile, a light, a chip) the moment the reader points at it, focuses it or taps it: a small card
 * with what it is and at most three signals, each a dot of its colour, a word and a value. Short: the page explains, the card names. A tap on a phone shows it first, a second tap
 * follows a link; Escape, a tap elsewhere or leaving the trigger closes it. The card is a tooltip (`aria-describedby`), never in the
 * way: it does not take the pointer, and it stays within the viewport.
 */
export function Hint({
  heading,
  swatch,
  meta,
  rows,
  children,
  render,
}: {
  heading: ReactNode
  /** The level's class (`lvl-recent`, `ai`, ...): the card shows the colour it explains. */
  swatch?: string
  /** A short fact at the card's top right: a span (≤6mo), a count. */
  meta?: ReactNode
  /** The signals, at most three: a dot of the colour, the signal in a word or two, its value. */
  rows?: HintRow[]
  /** One short line, for a hint that is not a list of signals. */
  children?: ReactNode
  render: (trigger: HintTrigger) => ReactNode
}) {
  const id = useId()
  const [open, setOpen] = useState(false)
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const card = useRef<HTMLDivElement>(null)

  const show = useCallback(() => {
    window.dispatchEvent(new CustomEvent(opened, { detail: id }))
    setOpen(true)
  }, [id])

  useEffect(() => {
    if (!open) return
    const other = (e: Event) => {
      if ((e as CustomEvent<string>).detail !== id) setOpen(false)
    }
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false)
    }
    const away = (e: Event) => {
      if (!anchor?.contains(e.target as Node)) setOpen(false)
    }
    const hide = () => setOpen(false)
    window.addEventListener(opened, other)
    document.addEventListener("keydown", key)
    document.addEventListener("pointerdown", away)
    window.addEventListener("scroll", hide, { passive: true, capture: true })
    window.addEventListener("resize", hide)
    return () => {
      window.removeEventListener(opened, other)
      document.removeEventListener("keydown", key)
      document.removeEventListener("pointerdown", away)
      window.removeEventListener("scroll", hide, { capture: true })
      window.removeEventListener("resize", hide)
    }
  }, [open, id, anchor])

  // Placed above the trigger, or below when there is no room, and kept inside the viewport. Positions go through the CSSOM: the
  // content security policy forbids style attributes, not this.
  useLayoutEffect(() => {
    const el = card.current
    const at = anchor
    if (!open || !el || !at) return
    const r = at.getBoundingClientRect()
    const w = el.offsetWidth
    const h = el.offsetHeight
    const above = r.top - gap - h >= gap
    const left = Math.min(Math.max(r.left + r.width / 2 - w / 2, gap), window.innerWidth - w - gap)
    el.style.left = `${Math.round(left)}px`
    el.style.top = `${Math.round(above ? r.top - gap - h : r.bottom + gap)}px`
    el.dataset.side = above ? "top" : "bottom"
  }, [open, anchor])

  const trigger: HintTrigger = {
    ref: setAnchor,
    onPointerDown: (e) => {
      // A finger's press comes before the focus and the click it causes: it is kept on the element, read at once by both.
      if (e.currentTarget instanceof HTMLElement) e.currentTarget.dataset.touch = String(e.pointerType !== "mouse")
    },
    onPointerEnter: (e) => {
      if (e.pointerType === "mouse") show()
    },
    onPointerLeave: (e) => {
      if (e.pointerType === "mouse") setOpen(false)
    },
    onFocus: (e) => {
      if (tapped(e.currentTarget)) return // the click decides
      show()
    },
    onBlur: () => setOpen(false),
    onClick: (e) => {
      // A tap shows the hint first; with it open, a second tap does what the element does.
      const touch = tapped(e.currentTarget)
      if (e.currentTarget instanceof HTMLElement) delete e.currentTarget.dataset.touch
      if (touch && !open) {
        e.preventDefault()
        show()
      }
    },
    "aria-describedby": open ? id : undefined,
  }

  return (
    <>
      {render(trigger)}
      {open &&
        createPortal(
          <div
            ref={card}
            id={id}
            role="tooltip"
            className="hint pointer-events-none fixed top-0 left-0 z-50 w-max max-w-[min(17rem,calc(100vw-1rem))] rounded-lg border border-border bg-popover/95 px-2.5 py-2 text-xs leading-relaxed text-popover-foreground shadow-xl backdrop-blur-md"
          >
            <div className="flex items-center gap-2">
              {swatch && <span aria-hidden="true" className={cn(swatch, "hint-swatch")} />}
              <span className="min-w-0 font-medium text-foreground">{heading}</span>
              {meta && <span className="ml-auto pl-3 font-mono whitespace-nowrap text-muted-foreground">{meta}</span>}
            </div>
            {rows && rows.length > 0 && (
              <ul className="mt-1.5 flex flex-col gap-1">
                {rows.slice(0, 3).map((r) => (
                  <li key={r.label} className="flex items-center gap-2">
                    <span aria-hidden="true" className={cn(r.swatch, "hint-dot")} />
                    <span>{r.label}</span>
                    {r.value && <span className="ml-auto pl-3 font-mono whitespace-nowrap text-muted-foreground">{r.value}</span>}
                  </li>
                ))}
              </ul>
            )}
            {children && <div className="mt-1 text-muted-foreground">{children}</div>}
          </div>,
          document.body,
        )}
    </>
  )
}
