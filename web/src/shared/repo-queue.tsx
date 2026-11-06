"use client"

import Link from "next/link"
import type { ComponentProps, ReactNode } from "react"
import { Card, CardTitle } from "@/components/ui/card"
import { count, plural } from "@/lib/format"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { ProblemAlert } from "@/shared/problem-alert"

/**
 * RepoQueue is a repository page's panel of what waits (its open pull requests, its open issues): the counts by state, the list
 * the caller draws, and the ways to all of them in the portal and on GitHub. A line saying so when none is open.
 */
export function RepoQueue({
  id,
  title,
  data,
  error,
  states,
  list,
  all,
  github,
}: {
  id: string
  title: string
  data?: {
    freshness: ComponentProps<typeof FreshnessBadge>["freshness"]
    counts: { open: number; states: Record<string, number> }
  }
  error: Error | null
  /** the states in the order they are told, with their words */
  states: readonly { state: string; label: string }[]
  /** the list that waits longest, drawn */
  list: ReactNode
  /** the portal's page with every one */
  all: string
  /** the same on GitHub, once one is known */
  github?: string
}) {
  return (
    <Card aria-labelledby={id}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id={id}>{title}</CardTitle>
        {data && <FreshnessBadge freshness={data.freshness} />}
      </div>
      {error && !data && <ProblemAlert error={error} />}
      {data && data.counts.open === 0 && <p className="mt-1 text-sm text-muted-foreground">None open.</p>}
      {data && data.counts.open > 0 && (
        <>
          <p className="mt-1 text-sm text-muted-foreground">
            {plural(data.counts.open, "open", "open")}:{" "}
            {states
              .map((s) => ({ ...s, n: data.counts.states[s.state] ?? 0 }))
              .filter((s) => s.n > 0)
              .map((s) => `${s.label} ${count(s.n)}`)
              .join(" · ")}
          </p>
          {list}
          <div className="mt-2 flex flex-wrap gap-x-4 text-sm">
            <Link href={all} className="hover:underline">
              All {count(data.counts.open)} open
            </Link>
            {github && (
              <a href={github} rel="noreferrer" className="text-muted-foreground hover:underline">
                On GitHub
              </a>
            )}
          </div>
        </>
      )}
    </Card>
  )
}
