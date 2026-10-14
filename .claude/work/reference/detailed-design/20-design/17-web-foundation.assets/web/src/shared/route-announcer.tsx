import { useRouterState } from "@tanstack/react-router"
import { useEffect, useRef, useState } from "react"

/** Moves focus to the page <h1> and announces the new title after each navigation (SPA a11y baseline). */
export function RouteAnnouncer() {
  // the pathname only: a search-param change on the same page (typing in a URL-bound filter) must not move focus away from the input
  const resolved = useRouterState({ select: (s) => s.resolvedLocation?.pathname })
  const first = useRef(true)
  const [message, setMessage] = useState("")
  useEffect(() => {
    if (resolved === undefined) return
    if (first.current) {
      first.current = false
      return
    }
    const h1 = document.querySelector<HTMLElement>("main h1")
    h1?.setAttribute("tabindex", "-1")
    h1?.focus({ preventScroll: false })
    setMessage(document.title)
  }, [resolved])
  return (
    <div className="sr-only" role="status" aria-live="polite" aria-atomic="true">
      {message}
    </div>
  )
}
