"use client"

import { useQuery } from "@tanstack/react-query"
import { useRef } from "react"
import { api } from "@/lib/api/client"
import { issuesQuery } from "@/lib/api/queries"
import { count } from "@/lib/format"
import type { Work } from "@/lib/preferences"
import { IssueList, issueStateOf } from "@/entities/issue/issue"
import { byWork, issuesFilter, moreIssuesQuery } from "./api"
import { useMore } from "./more"
import { QueueSection, queueKey } from "./queue-section"
import type { Section } from "./side"

/**
 * Issues is the issues element (design/layout.md): the open issues of the work that counts, the newest first, each where it stands
 * and its kind, repository and number, who and when; a line of how many need triage and how many nobody took; "All N open" goes to
 * /issues. Nothing when none is open. As a section of the dashboard's side panel (section) its title folds it and its list scrolls
 * inside it; drawn as a section of the presentation page's side column (column), its rows are the latest commits' two lines.
 */
export function Issues({
  work,
  group,
  n,
  section,
  column = false,
}: {
  work: Work
  /** one product's, else the fleet's */
  group?: string
  n: number
  section?: Section
  /** drawn as a section of the presentation page's side column, as its latest commits are */
  column?: boolean
}) {
  const f = issuesFilter(work, n, group)
  const { data } = useQuery(issuesQuery(api, f))
  const first = data?.issues ?? []
  const end = useRef<HTMLDivElement>(null)
  const more = useMore(
    moreIssuesQuery(api, f),
    first,
    (p) => p.issues,
    queueKey,
    end,
    section?.open !== false && first.length >= n,
  )
  const counts = data?.counts
  if (counts?.open === 0 && !section) return null
  const q = new URLSearchParams()
  if (group) q.set("group", group)
  if (byWork(work)) q.set("by", "people")
  return (
    <QueueSection
      id="issues"
      title="Issues"
      all={{ href: `/issues${q.size > 0 ? `?${q}` : ""}`, text: counts ? `All ${count(counts.open)} open` : "All open" }}
      section={section}
      column={column}
      summary={
        !counts
          ? "Loading…"
          : counts.open === 0
            ? "None open"
            : `${count(counts.states.triage)} need triage, ${count(counts.states.unassigned)} nobody took`
      }
      list={more.items.length > 0 && <IssueList issues={more.items} compact />}
      rows={more.items.map((i) => ({ ...i, at: i.createdAt, state: issueStateOf(i.state) }))}
      end={end}
      loading={more.loading}
      done={more.done}
      empty={counts ? "None open" : "Loading…"}
      rest="Every issue, with filters"
    />
  )
}
