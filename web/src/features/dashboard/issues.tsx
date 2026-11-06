"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useRef } from "react"
import { Card } from "@/components/ui/card"
import { api } from "@/lib/api/client"
import { issuesQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { count } from "@/lib/format"
import type { Work } from "@/lib/preferences"
import { IssueList, issueStateOf } from "@/entities/issue/issue"
import { ColumnSection } from "./column"
import { byWork, issuesFilter, moreIssuesQuery } from "./api"
import { useMore } from "./more"
import { sectionBody, sectionCard, SectionFold, SectionTitle, type Section } from "./side"

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
  const more = useMore(moreIssuesQuery(api, f), first, (p) => p.issues, key, end, section?.open !== false && first.length >= n)
  const counts = data?.counts
  if (counts?.open === 0 && !section) return null
  const q = new URLSearchParams()
  if (group) q.set("group", group)
  if (byWork(work)) q.set("by", "people")
  const all = `/issues${q.size > 0 ? `?${q}` : ""}`
  if (column) {
    return (
      <ColumnSection
        id="issues"
        title="Issues"
        all={{ href: all, text: counts ? `All ${count(counts.open)} open` : "All open" }}
        section={section}
        rows={more.items.map((i) => ({ ...i, at: i.createdAt, state: issueStateOf(i.state) }))}
        end={end}
        loading={more.loading}
        done={more.done}
        empty={counts ? "None open" : "Loading…"}
        rest="Every issue, with filters"
      />
    )
  }
  return (
    <Card aria-labelledby="issues" className={cn("flex flex-col gap-2", sectionCard(section))}>
      <div className="flex items-center gap-2">
        <SectionTitle id="issues">Issues</SectionTitle>
        <Link href={all} className="text-sm whitespace-nowrap text-muted-foreground hover:text-foreground hover:underline">
          {counts ? `All ${count(counts.open)} open` : "All open"}
        </Link>
        <SectionFold id="issues" section={section} />
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
          {more.items.length > 0 && <IssueList issues={more.items} compact />}
          <div ref={end} aria-live="polite" className="text-center text-xs text-muted-foreground">
            {more.loading ? "Loading more…" : null}
            {more.done ? (
              <Link href={all} className="hover:text-primary hover:underline">
                Every issue, with filters
              </Link>
            ) : null}
          </div>
        </div>
      )}
    </Card>
  )
}

/** key is one issue once, however many pages it came in. */
const key = (x: { repo: string; number: number }) => `${x.repo}#${x.number}`
