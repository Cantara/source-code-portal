"use client"

import { useEffect, useRef, useState, useSyncExternalStore, type KeyboardEvent, type PointerEvent } from "react"
import { cn } from "@/lib/cn"
import { columnWidths, profiles, type Profile } from "@/lib/preferences"
import { apply, useTheme, type Choice } from "@/lib/theme"

/** barFor is how long a tap shows the controls. */
const barFor = 5_000

const themes: Choice[] = ["dark", "light", "system"]

/** columns are the landscape grid at each width of the side column, as written classes (the CSP allows no style attribute). */
export const columns: Record<number, string> = {
  20: "split:grid-cols-[minmax(0,1fr)_0.75rem_20%]",
  25: "split:grid-cols-[minmax(0,1fr)_0.75rem_25%]",
  30: "split:grid-cols-[minmax(0,1fr)_0.75rem_30%]",
  35: "split:grid-cols-[minmax(0,1fr)_0.75rem_35%]",
  40: "split:grid-cols-[minmax(0,1fr)_0.75rem_40%]",
  45: "split:grid-cols-[minmax(0,1fr)_0.75rem_45%]",
  50: "split:grid-cols-[minmax(0,1fr)_0.75rem_50%]",
  55: "split:grid-cols-[minmax(0,1fr)_0.75rem_55%]",
  60: "split:grid-cols-[minmax(0,1fr)_0.75rem_60%]",
}

export const clampColumn = (w: number) =>
  Math.min(columnWidths.max, Math.max(columnWidths.min, Math.round(w / columnWidths.step) * columnWidths.step))

/** A press that moves less than this is a tap, not a drag; two taps within doubleTap bring back the usual width. */
const slop = 4
const doubleTap = 400

/**
 * Resizer is the side column's edge in split: dragged (a finger or a pointer) or moved with the arrow keys, in steps of 5% of
 * the screen from 20% to 60%; a double tap or click brings back the profile's usual width. Taps are told apart here, as a touch on a handle
 * that takes the whole gesture (touch-action none) sends no dblclick in every browser, and a tap never moves the column.
 */
export function Resizer({
  width,
  usual,
  onChange,
}: {
  width: number
  /** the profile's width, which a double tap brings back */
  usual: number
  onChange?: (width: number, keep: boolean) => void
}) {
  const press = useRef<{ x: number; moved: boolean } | null>(null)
  const lastTap = useRef(Number.NEGATIVE_INFINITY)
  const at = (e: PointerEvent<HTMLDivElement>) => {
    const frame = e.currentTarget.parentElement?.getBoundingClientRect()
    return frame ? clampColumn(((frame.right - e.clientX) / frame.width) * 100) : width
  }
  const key = (e: KeyboardEvent<HTMLDivElement>) => {
    const to = {
      ArrowLeft: width + columnWidths.step,
      ArrowRight: width - columnWidths.step,
      Home: columnWidths.max,
      End: columnWidths.min,
    }[e.key]
    if (to === undefined || !onChange) return
    e.preventDefault()
    onChange(clampColumn(to), true)
  }
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the side column"
      aria-valuemin={columnWidths.min}
      aria-valuemax={columnWidths.max}
      aria-valuenow={width}
      aria-valuetext={`${width}% of the screen`}
      tabIndex={onChange ? 0 : -1}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId)
        press.current = { x: e.clientX, moved: false }
      }}
      onPointerMove={(e) => {
        const p = press.current
        if (!p || !e.currentTarget.hasPointerCapture(e.pointerId)) return
        if (!p.moved && Math.abs(e.clientX - p.x) < slop) return
        p.moved = true
        onChange?.(at(e), false)
      }}
      onPointerUp={(e) => {
        e.currentTarget.releasePointerCapture(e.pointerId)
        const p = press.current
        press.current = null
        if (p?.moved) {
          onChange?.(at(e), true)
          lastTap.current = Number.NEGATIVE_INFINITY
          return
        }
        if (e.timeStamp - lastTap.current < doubleTap) {
          onChange?.(usual, true)
          lastTap.current = Number.NEGATIVE_INFINITY
        } else lastTap.current = e.timeStamp
      }}
      onPointerCancel={() => {
        press.current = null
      }}
      onKeyDown={key}
      title="Drag to resize; double-tap for the usual width"
      // As wide as the gap it replaces, a hairline to see; the hit area reaches past it for a finger.
      className="group relative hidden cursor-col-resize touch-none justify-center outline-none before:absolute before:inset-y-0 before:-inset-x-2 before:content-[''] split:flex"
    >
      <span className="my-6 w-px rounded-full bg-border transition-all group-hover:w-0.5 group-hover:bg-primary group-focus-visible:w-0.5 group-focus-visible:bg-primary group-active:w-0.5 group-active:bg-primary" />
    </div>
  )
}

/** Bar is the controls a tap or a key shows for a few seconds: back to the dashboard, the theme, full screen, the design mode. */
export function Bar({
  profile,
  size,
  wall,
  onWall,
  onDesign,
}: {
  profile: Profile
  /** the window's size in CSS pixels, so two people can tell they look at the same screen */
  size?: string
  wall: boolean
  onWall: (wall: boolean) => void
  onDesign?: () => void
}) {
  const [shown, setShown] = useState(false)
  const [within, setWithin] = useState(false)
  const choice = useTheme()
  const [full, setFull] = useState(false)
  // Full screen where the browser has it (not the iPhone), known only in the browser: the server renders without it.
  const canFull = useSyncExternalStore(
    () => () => undefined,
    () => document.fullscreenEnabled,
    () => false,
  )
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const wake = () => {
      setShown(true)
      clearTimeout(timer)
      timer = setTimeout(() => setShown(false), barFor)
    }
    const screen = () => setFull(!!document.fullscreenElement)
    document.addEventListener("pointerdown", wake)
    document.addEventListener("keydown", wake)
    document.addEventListener("fullscreenchange", screen)
    return () => {
      clearTimeout(timer)
      document.removeEventListener("pointerdown", wake)
      document.removeEventListener("keydown", wake)
      document.removeEventListener("fullscreenchange", screen)
    }
  }, [])
  const next = themes[(themes.indexOf(choice) + 1) % themes.length] ?? "system"
  const button = "rounded-md px-2.5 py-1.5 text-sm hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring outline-none"
  return (
    <nav
      aria-label="Presentation"
      onFocus={() => setWithin(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) setWithin(false)
      }}
      className={cn(
        "fixed bottom-3 left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-lg border border-border bg-popover/95 p-1 text-popover-foreground shadow-xl backdrop-blur-md transition-opacity motion-reduce:transition-none",
        shown || within ? "opacity-100" : "pointer-events-none opacity-0",
      )}
    >
      {/* The profile this screen is in, and the wall it can be told it is (a wall has a desk's pixels). */}
      <span className="px-2 text-xs font-medium tracking-widest text-muted-foreground uppercase">
        {profiles.find((x) => x.value === profile)?.label}
        {size && <span className="ml-1.5 font-mono font-normal tracking-normal normal-case">{size}</span>}
      </span>
      <button type="button" className={button} aria-pressed={wall} onClick={() => onWall(!wall)}>
        {wall ? "Not a wall" : "On a wall"}
      </button>
      {/* A whole page load: the dashboard draws its own theme and header. */}
      <a href="/" className={button}>
        Dashboard
      </a>
      <button type="button" className={button} onClick={() => apply(next)}>
        Theme: {choice === "system" ? "auto" : choice}
      </button>
      {canFull && (
        <button
          type="button"
          className={button}
          onClick={() => {
            // The browser may refuse (not allowed here, the gesture is old): the button then does what it can, quietly.
            void (full ? document.exitFullscreen() : document.documentElement.requestFullscreen()).catch(() => undefined)
          }}
        >
          {full ? "Leave full screen" : "Full screen"}
        </button>
      )}
      {onDesign && (
        <button type="button" className={button} onClick={onDesign}>
          Design this page
        </button>
      )}
    </nav>
  )
}
