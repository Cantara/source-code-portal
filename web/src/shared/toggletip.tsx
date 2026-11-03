"use client"

import { useEffect, useId, useRef, useState, type ReactNode } from "react"
import { cn } from "@/lib/cn"

/**
 * Toggletip is an ⓘ button that shows a short explanation beside it: opened by a click or a tap (no hover, so it works on a phone
 * and from the keyboard), closed by the button again, Escape or a click outside. It never blocks the page.
 */
export function Toggletip({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  const [open, setOpen] = useState(false)
  const id = useId()
  const root = useRef<HTMLSpanElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return
      setOpen(false)
      button.current?.focus()
    }
    const click = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("keydown", key)
    document.addEventListener("pointerdown", click)
    return () => {
      document.removeEventListener("keydown", key)
      document.removeEventListener("pointerdown", click)
    }
  }, [open])
  return (
    <span ref={root} className={cn("relative inline-flex", className)}>
      <button
        ref={button}
        type="button"
        aria-label={label}
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen(!open)}
        className="inline-flex size-5 items-center justify-center rounded-full text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      >
        <span aria-hidden="true">ⓘ</span>
      </button>
      <div
        id={id}
        role="note"
        aria-label={label}
        hidden={!open}
        className="absolute top-full left-0 z-20 mt-1 w-[min(22rem,calc(100vw-2rem))] rounded-md border border-border bg-popover p-3 text-xs leading-relaxed font-normal tracking-normal normal-case text-popover-foreground shadow-lg"
      >
        {children}
      </div>
    </span>
  )
}
