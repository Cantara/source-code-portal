"use client"

import { useSyncExternalStore } from "react"
import { forksCookie, forksShown } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { forget, remember } from "@/lib/preferences"

const never = () => () => {}

/** useForksShown reports whether this browser asks for the forks; the server renders without, the browser corrects it at hydration. */
export const useForksShown = () => useSyncExternalStore(never, forksShown, () => false)

/**
 * ForksSwitch offers the forks the portal leaves out unless asked: the switch of the reader's choice, kept in a cookie. It is absent
 * where no fork is hidden. The pages read their data with the choice, so the page is read again after it.
 */
export function ForksSwitch({ hidden }: { hidden: number }) {
  const shown = useForksShown()
  if (hidden === 0) return null
  return (
    <button
      type="button"
      aria-pressed={shown}
      onClick={() => {
        if (shown) forget(forksCookie)
        else remember(forksCookie, "show")
        location.reload()
      }}
      className={cn(
        "rounded-md border border-border px-2 py-1 text-xs",
        shown ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:text-foreground",
      )}
    >
      {shown ? "Showing forks" : `Show forks (${hidden})`}
    </button>
  )
}
