"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { Card } from "@/components/ui/card"
import { api } from "@/lib/api/client"
import { issuesQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { count } from "@/lib/format"
import type { Work } from "@/lib/preferences"
import { IssueList } from "@/entities/issue/issue"
import { byWork, issuesFilter } from "./api"
import { sectionBody, sectionCard, SectionTitle, type Section } from "./side"

/**
 * Issues is the issues element (design/layout.md): the open issues of the work that counts that wait first, each where it stands
 * and its kind, repository and number, who and when; a line of how many need triage and how many nobody took; "All N open" goes to
 * /issues. Nothing when none is open. As a section of the dashboard's side panel (section) its title folds it and its list scrolls
 * inside it; on a wall (wall) its rows run in columns.
 */
export function Issues({
  work,
  group,
  n,
  section,
  wall = false,
}: {
  work: Work
  /** one product's, else the fleet's */
  group?: string
  n: number
  section?: Section
  wall?: boolean
}) {
  const { data } = useQuery(issuesQuery(api, issuesFilter(work, n, group)))
  const counts = data?.counts
  if (counts?.open === 0 && !section) return null
  const q = new URLSearchParams()
  if (group) q.set("group", group)
  if (byWork(work)) q.set("by", "people")
  const all = `/issues${q.size > 0 ? `?${q}` : ""}`
  return (
    <Card aria-labelledby="issues" className={cn("flex flex-col gap-2", wall && "min-h-0 overflow-hidden", sectionCard(section))}>
      <div className="flex items-center gap-2">
        <SectionTitle id="issues" section={section}>
          Issues
        </SectionTitle>
        <Link href={all} className="text-sm whitespace-nowrap text-muted-foreground hover:text-foreground hover:underline">
          {counts ? `All ${count(counts.open)} open` : "All open"}
        </Link>
        {section?.fold}
      </div>
      {section?.open !== false && (
        <p className="text-sm text-muted-foreground">
          {!counts
            ? "Loading…"
            : counts.open === 0
              ? "None open"
              : `${count(counts.states.triage)} need triage, ${count(counts.states.unassigned)} nobody took`}
        </p>
      )}
      {section?.open !== false && (
        <div id="issues-body" className={cn("flex flex-col gap-2", sectionBody(section))}>
          {data && data.issues.length > 0 && (
            <IssueList
              issues={data.issues}
              compact
              className={wall ? "grid grid-cols-1 gap-x-6 divide-y-0 @3xl:grid-cols-2" : undefined}
            />
          )}
        </div>
      )}
    </Card>
  )
}
