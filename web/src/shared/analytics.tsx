"use client"

import { useEffect } from "react"
import { counted, scrub, type Analytics } from "@/lib/analytics"

// init() throws when it is called twice, and React runs an effect twice in development: module scope survives the remount.
let started = false

/**
 * Tracker starts Plausible's tracker, which is part of this bundle (no script from another host): its events are posted to the
 * portal's own address, which forwards them. Page addresses go without their query but the campaign's, and the presentation page
 * is not counted. Rendered only for a viewer who sees no private repository (the root layout).
 */
export function Tracker({ domain, path }: Analytics): null {
  useEffect(() => {
    if (started) return
    started = true
    void import("@plausible-analytics/tracker").then(({ init }) =>
      init({
        domain,
        endpoint: path,
        logging: false,
        transformRequest: (p) => (counted(p.u) ? { ...p, u: scrub(p.u) } : null),
      }),
    )
  }, [domain, path])
  return null
}
