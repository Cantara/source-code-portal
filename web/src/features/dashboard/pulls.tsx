"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useRef } from "react"
import { Card } from "@/components/ui/card"
import { api, type Schemas } from "@/lib/api/client"
import { pullsQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { count } from "@/lib/format"
import type { Work } from "@/lib/preferences"
import { byWork, pullsFilter, morePullsQuery } from "./api"
import { useMore } from "./more"
import { PullList } from "@/entities/pull/pull"
import { sectionBody, sectionCard, SectionFold, SectionTitle, type Section } from "./side"

/**
 * Pulls is the pulls element (design/pulls.md): the open pull requests of the work that counts, the newest first, each whose turn,
 * each its state, repository and number, who and when; "All N open" goes to /pulls. Nothing when
 * none is open. As a section of the dashboard's side panel (section) its title folds it and its list scrolls inside it; on a wall
 * (wall) its rows run in columns.
 */
export function Pulls({
  work,
  group,
  counts,
  n,
  section,
  wall = false,
}: {
  work: Work
  /** one product's, else the fleet's */
  group?: string
  /** the open pull requests of the work that counts, as the dashboard counted them */
  counts: Schemas["PullCounts"]
  n: number
  section?: Section
  wall?: boolean
}) {
  const f = pullsFilter(work, n, group)
  const { data } = useQuery(pullsQuery(api, f))
  const first = data?.pulls ?? []
  const end = useRef<HTMLDivElement>(null)
  const more = useMore(morePullsQuery(api, f), first, (p) => p.pulls, key, end, section?.open !== false && first.length >= n)
  if (counts.open === 0 && !section) return null
  const shown = more.items
  const q = new URLSearchParams()
  if (group) q.set("group", group)
  if (byWork(work)) q.set("by", "people")
  const all = `/pulls${q.size > 0 ? `?${q}` : ""}`
  return (
    <Card aria-labelledby="pulls" className={cn("flex flex-col gap-2", wall && "min-h-0 overflow-hidden", sectionCard(section))}>
      <div className="flex items-center gap-2">
        <SectionTitle id="pulls">Pull requests</SectionTitle>
        <Link href={all} className="text-sm whitespace-nowrap text-muted-foreground hover:text-foreground hover:underline">
          All {count(counts.open)} open
        </Link>
        <SectionFold id="pulls" section={section} />
      </div>
      {section?.open !== false && (
        <p className="text-sm text-muted-foreground">
          {counts.states.review > 0
            ? `${count(counts.states.review)} waiting for a review, ${count(counts.states.ready)} ready to merge`
            : counts.open > 0
              ? `None waiting for a review, ${count(counts.states.ready)} ready to merge`
              : "None open"}
        </p>
      )}
      {section?.open !== false && (
        <div id="pulls-body" className={cn("flex flex-col gap-2", sectionBody(section))}>
          {shown.length > 0 && (
            <PullList
              pulls={shown}
              compact
              className={wall ? "grid grid-cols-1 gap-x-6 divide-y-0 @3xl:grid-cols-2" : undefined}
            />
          )}
          <div ref={end} aria-live="polite" className="text-center text-xs text-muted-foreground">
            {more.loading ? "Loading more…" : null}
            {more.done ? (
              <Link href={all} className="hover:text-primary hover:underline">
                Every pull request, with filters
              </Link>
            ) : null}
          </div>
        </div>
      )}
    </Card>
  )
}

/** key is one pull request once, however many pages it came in. */
const key = (x: { repo: string; number: number }) => `${x.repo}#${x.number}`
