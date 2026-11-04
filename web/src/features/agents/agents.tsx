"use client"

import { usePathname, useRouter } from "next/navigation"
import { useEffect, useRef } from "react"
import type { Schemas } from "@/lib/api/client"
import { catalogueTools, modelContext, pageTools, register } from "./webmcp"

/**
 * WebMCP offers the browser's agent the portal's tools while the portal is open (design/agents.md). It draws nothing, and without
 * WebMCP in the browser it does nothing and fetches nothing. The tools are registered once: the page's own tools read where the
 * reader is when they run.
 */
export function WebMCP() {
  const router = useRouter()
  const pathname = usePathname()
  const where = useRef(pathname)
  useEffect(() => {
    where.current = pathname
  }, [pathname])

  useEffect(() => {
    const mc = modelContext()
    if (!mc) return
    const ac = new AbortController()
    register(
      mc,
      pageTools({
        path: () => where.current,
        title: () => document.title,
        open: (p) => router.push(p),
        refresh: () => router.refresh(),
      }),
      ac.signal,
    )
    fetch(`${location.origin}/api/v1/tools`, { headers: { Accept: "application/json" }, signal: ac.signal })
      .then((r) => (r.ok ? (r.json() as Promise<Schemas["ToolList"]>) : undefined))
      .then((list) => {
        if (list && !ac.signal.aborted) register(mc, catalogueTools(list), ac.signal)
      })
      .catch(() => undefined)
    return () => ac.abort()
  }, [router])

  return null
}
