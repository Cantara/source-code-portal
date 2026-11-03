"use client"

import { useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react"
import { PanelRightIcon } from "@/components/ui/icons"
import { cn } from "@/lib/cn"
import { remember, sideCookie, sideValue, sideWidths, type Side } from "@/lib/preferences"

// The panel's widths as classes, one per rem: the server renders the remembered width, and the content security policy allows no
// style attribute. Tailwind finds them here as written.
const widths: Record<number, string> = {
  18: "lg:w-[18rem]",
  19: "lg:w-[19rem]",
  20: "lg:w-[20rem]",
  21: "lg:w-[21rem]",
  22: "lg:w-[22rem]",
  23: "lg:w-[23rem]",
  24: "lg:w-[24rem]",
  25: "lg:w-[25rem]",
  26: "lg:w-[26rem]",
  27: "lg:w-[27rem]",
  28: "lg:w-[28rem]",
  29: "lg:w-[29rem]",
  30: "lg:w-[30rem]",
  31: "lg:w-[31rem]",
  32: "lg:w-[32rem]",
  33: "lg:w-[33rem]",
  34: "lg:w-[34rem]",
  35: "lg:w-[35rem]",
  36: "lg:w-[36rem]",
  37: "lg:w-[37rem]",
  38: "lg:w-[38rem]",
  39: "lg:w-[39rem]",
  40: "lg:w-[40rem]",
}

const clamp = (w: number) => Math.min(Math.max(Math.round(w), sideWidths.min), sideWidths.max)

/**
 * SidePanel puts aside (the latest commits) at the right of main (the products) on a wide screen, sticky while the products scroll:
 * folded to a rail by its button, and as wide as its edge is dragged (or moved with the arrow keys), both remembered in the cookie
 * `scp-side-commits`. On a narrow screen aside follows main, and only folds.
 */
export function SidePanel({
  main,
  aside,
  initial,
  label,
}: {
  main: ReactNode
  aside: (fold: ReactNode) => ReactNode
  initial: Side
  label: string
}) {
  const [side, setSide] = useState(initial)
  const root = useRef<HTMLDivElement>(null)
  const save = (s: Side) => {
    setSide(s)
    remember(sideCookie, sideValue(s))
  }
  const resizeTo = (width: number) => setSide((s) => ({ ...s, width: clamp(width) }))

  const drag = (e: PointerEvent<HTMLDivElement>) => {
    if (!e.currentTarget.hasPointerCapture(e.pointerId)) return
    const right = root.current?.getBoundingClientRect().right ?? 0
    const rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
    resizeTo((right - e.clientX) / rem)
  }
  const key = (e: KeyboardEvent) => {
    const to = { ArrowLeft: side.width + 1, ArrowRight: side.width - 1, Home: sideWidths.max, End: sideWidths.min }[e.key]
    if (to === undefined) return
    e.preventDefault()
    save({ ...side, width: clamp(to) })
  }

  const fold = (
    <button
      type="button"
      onClick={() => save({ ...side, open: false })}
      aria-expanded={true}
      aria-label={`Hide ${label.toLowerCase()}`}
      title={`Hide ${label.toLowerCase()}`}
      className="grid size-7 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
    >
      <PanelRightIcon open className="size-4" />
    </button>
  )

  return (
    <div ref={root} className="flex flex-col gap-6 lg:flex-row lg:items-start lg:gap-0">
      <div className="min-w-0 flex-1">{main}</div>
      {side.open ? (
        <>
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label={`Resize ${label.toLowerCase()}`}
            aria-valuemin={sideWidths.min}
            aria-valuemax={sideWidths.max}
            aria-valuenow={side.width}
            tabIndex={0}
            onPointerDown={(e) => e.currentTarget.setPointerCapture(e.pointerId)}
            onPointerMove={drag}
            onPointerUp={(e) => {
              e.currentTarget.releasePointerCapture(e.pointerId)
              save(side)
            }}
            onDoubleClick={() => save({ ...side, width: sideWidths.initial })}
            onKeyDown={key}
            title="Drag to resize; double-click for the usual width"
            className="group hidden w-6 shrink-0 cursor-col-resize touch-none self-stretch justify-center outline-none lg:flex"
          >
            <span className="my-2 w-px rounded-full bg-border transition-colors group-hover:w-0.5 group-hover:bg-primary group-focus-visible:w-0.5 group-focus-visible:bg-primary group-active:bg-primary" />
          </div>
          <aside
            aria-label={label}
            className={cn(
              "min-w-0 shrink-0 lg:sticky lg:top-20 lg:max-h-[calc(100dvh-6rem)] lg:overflow-y-auto",
              widths[side.width],
            )}
          >
            {aside(fold)}
          </aside>
        </>
      ) : (
        <aside aria-label={label} className="shrink-0 lg:sticky lg:top-20 lg:ml-4">
          <button
            type="button"
            onClick={() => save({ ...side, open: true })}
            aria-expanded={false}
            aria-label={`Show ${label.toLowerCase()}`}
            title={`Show ${label.toLowerCase()}`}
            className="panel flex w-full items-center gap-2 px-3 py-2 text-xs font-medium tracking-widest text-muted-foreground uppercase transition-colors hover:text-foreground lg:w-10 lg:flex-col lg:px-0 lg:py-3"
          >
            <PanelRightIcon open={false} className="size-4 shrink-0" />
            <span className="lg:[writing-mode:vertical-rl]">{label}</span>
          </button>
        </aside>
      )}
    </div>
  )
}
