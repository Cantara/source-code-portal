"use client"

import { useQuery } from "@tanstack/react-query"
import { useRef } from "react"
import { api, type Schemas } from "@/lib/api/client"
import { pullsQuery } from "@/lib/api/queries"
import { count } from "@/lib/format"
import type { Work } from "@/lib/preferences"
import { byWork, pullsFilter, morePullsQuery } from "./api"
import { useMore } from "./more"
import { PullList, stateOf } from "@/entities/pull/pull"
import { QueueSection, queueKey } from "./queue-section"
import type { Section } from "./side"

/**
 * Pulls is the pulls element (design/pulls.md): the open pull requests of the work that counts, the newest first, each with whose turn,
 * its state, repository and number, who and when; "All N open" goes to /pulls. Nothing when none is open. As a section of the
 * dashboard's side panel (section) its title folds it and its list scrolls inside it; drawn as a section of the presentation page's
 * side column (column), its rows are the latest commits' two lines.
 */
export function Pulls({
  work,
  group,
  counts,
  n,
  section,
  column = false,
}: {
  work: Work
  /** one product's, else the fleet's */
  group?: string
  /** the open pull requests of the work that counts, as the dashboard counted them */
  counts: Schemas["PullCounts"]
  n: number
  section?: Section
  /** drawn as a section of the presentation page's side column, as its latest commits are */
  column?: boolean
}) {
  const f = pullsFilter(work, n, group)
  const { data } = useQuery(pullsQuery(api, f))
  const first = data?.pulls ?? []
  const end = useRef<HTMLDivElement>(null)
  const more = useMore(morePullsQuery(api, f), first, (p) => p.pulls, queueKey, end, section?.open !== false && first.length >= n)
  if (counts.open === 0 && !section) return null
  const q = new URLSearchParams()
  if (group) q.set("group", group)
  if (byWork(work)) q.set("by", "people")
  return (
    <QueueSection
      id="pulls"
      title="Pull requests"
      all={{ href: `/pulls${q.size > 0 ? `?${q}` : ""}`, text: `All ${count(counts.open)} open` }}
      section={section}
      column={column}
      summary={
        counts.states.review > 0
          ? `${count(counts.states.review)} waiting for a review, ${count(counts.states.ready)} ready to merge`
          : counts.open > 0
            ? `None waiting for a review, ${count(counts.states.ready)} ready to merge`
            : "None open"
      }
      list={more.items.length > 0 && <PullList pulls={more.items} compact />}
      rows={more.items.map((p) => ({ ...p, at: p.createdAt, state: stateOf(p.state) }))}
      end={end}
      loading={more.loading}
      done={more.done}
      empty="None open"
      rest="Every pull request, with filters"
    />
  )
}
