"use client"

import Link from "next/link"
import { useEffect, useId, useRef, useState } from "react"
import { ChevronDownIcon } from "@/components/ui/icons"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { count } from "@/lib/format"
import { by, commitsCookie, forget, kinds, remember, sameWork, type Work, type WorkKind } from "@/lib/preferences"
import { window } from "@/lib/rules"
import { useRules } from "@/lib/rules-context"
import { maintenance } from "./work"

/** dots are the kinds' colours, as the sparklines and the signals draw them. */
const dots: Record<WorkKind, string> = { hand: "bg-primary", ai: "bg-[var(--ai)]", automation: "bg-muted-foreground" }

/** useShow is the kinds the latest commits show: the reader's own choice (initial, from the cookie), else the work that counts. */
export function useShow(initial: Work | undefined, work: Work) {
  const [own, setOwn] = useState(initial)
  const shown = own ?? work
  const set = (w: Work | undefined) => {
    const next = w && !sameWork(w, work) ? w : undefined
    setOwn(next)
    if (next) remember(commitsCookie, next.join(","))
    else forget(commitsCookie)
  }
  return { shown, follows: own === undefined, set }
}

/**
 * ShowPicker is the latest commits' label as a button: the kinds of work they show, in words ("by the team or automation"). It
 * opens a small dialog of the three kinds as checkboxes, each in its colour with its commits in the rules' window and what it
 * covers; one stays checked. The choice is the list's own (the cookie scp-latest-work) and can go back to the work that counts,
 * which Settings choose and the rest of the page counts.
 */
export function ShowPicker({
  shown,
  follows,
  activity,
  onChange,
  align = "end",
  className,
}: {
  shown: Work
  follows: boolean
  activity: Schemas["Activity"]
  onChange: (w: Work | undefined) => void
  /** the side of the button the dialog lines up with: its end (a label at the right of its header) or its start */
  align?: "start" | "end"
  className?: string
}) {
  const rules = useRules()
  const [open, setOpen] = useState(false)
  const id = useId()
  const root = useRef<HTMLDivElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const first = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (!open) return
    first.current?.focus()
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return
      setOpen(false)
      button.current?.focus()
    }
    const away = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("keydown", key)
    document.addEventListener("pointerdown", away)
    return () => {
      document.removeEventListener("keydown", key)
      document.removeEventListener("pointerdown", away)
    }
  }, [open])
  const counts: Record<WorkKind, number> = { hand: activity.hand, ai: activity.ai, automation: maintenance(activity) }
  const toggle = (k: WorkKind, on: boolean) => {
    const next = kinds.map((x) => x.value).filter((x) => (x === k ? on : shown.includes(x)))
    if (next.length > 0) onChange(next)
  }
  return (
    <div ref={root} className={cn("relative min-w-0", className)}>
      <button
        ref={button}
        type="button"
        aria-label={`Show commits ${by(shown)}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen(!open)}
        className="flex max-w-full min-w-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      >
        <span className="flex shrink-0 items-center gap-1" aria-hidden="true">
          {kinds
            .filter((k) => shown.includes(k.value))
            .map((k) => (
              <span key={k.value} className={cn("size-1.5 rounded-full", dots[k.value])} />
            ))}
        </span>
        <span className="truncate">{by(shown)}</span>
        <ChevronDownIcon className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-180")} />
      </button>
      {open && (
        <div
          id={id}
          role="dialog"
          aria-label="Show in the latest commits"
          className={cn(
            "absolute top-full z-30 mt-1.5 w-[min(19rem,calc(100vw-2rem))] rounded-xl border border-border bg-popover/95 p-3 text-popover-foreground shadow-2xl backdrop-blur-md",
            align === "end" ? "right-0" : "left-0",
          )}
        >
          <p className="flex items-baseline justify-between gap-2 text-xs text-muted-foreground">
            <span className="font-medium tracking-widest uppercase">Show commits</span>
            <span>last {window(rules)}</span>
          </p>
          <fieldset className="mt-2 flex flex-col gap-1">
            <legend className="sr-only">The kinds of work the latest commits show</legend>
            {kinds.map((k, i) => {
              const on = shown.includes(k.value)
              const last = on && shown.length === 1
              return (
                <label
                  key={k.value}
                  className={cn(
                    "flex cursor-pointer items-start gap-2.5 rounded-lg px-2 py-1.5 transition-colors hover:bg-accent",
                    last && "cursor-default",
                  )}
                >
                  <input
                    ref={i === 0 ? first : undefined}
                    type="checkbox"
                    checked={on}
                    disabled={last}
                    aria-describedby={`${id}-${k.value}`}
                    onChange={(e) => toggle(k.value, e.target.checked)}
                    className="mt-0.5 size-4 accent-primary"
                  />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="flex items-center gap-1.5 text-sm font-medium">
                      <span aria-hidden="true" className={cn("size-2 rounded-full", dots[k.value])} />
                      {k.label}
                      <span className="ml-auto font-mono text-xs font-normal text-muted-foreground tabular-nums">
                        {count(counts[k.value])}
                      </span>
                    </span>
                    <span id={`${id}-${k.value}`} className="text-xs text-muted-foreground">
                      {k.about}
                    </span>
                  </span>
                </label>
              )
            })}
          </fieldset>
          <p className="mt-2 border-t border-border pt-2 text-right text-xs text-muted-foreground">
            {follows ? (
              <Link href="/settings" className="hover:text-foreground hover:underline">
                The work that counts, in Settings
              </Link>
            ) : (
              <button
                type="button"
                onClick={() => onChange(undefined)}
                className="rounded-sm hover:text-foreground hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              >
                Same as the work that counts
              </button>
            )}
          </p>
        </div>
      )}
    </div>
  )
}
