"use client"

import Link from "next/link"
import { useRef } from "react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/cn"
import { api, type Commit, type Schemas } from "@/lib/api/client"
import { CommitList } from "@/entities/commit/commit-list"
import { sameWork, type Work } from "@/lib/preferences"
import { moreQuery } from "./api"
import { useMore } from "./more"
import { ShowPicker, useShow } from "./show"
import { sectionBody, sectionCard, SectionFold, SectionTitle, type Section } from "./side"

/** commitKey is one commit once, however many pages it came in: new commits move older ones to later pages while the reader scrolls. */
export const commitKey = (c: Commit) => `${c.repo}@${c.sha}`

/**
 * LatestCommits are the newest commits of the kinds the reader shows them (ShowPicker: the work that counts until they choose).
 * Scrolled to its end, the list reads the next page of the same commits and goes on, up to morePages pages; past them "All commits"
 * has the rest. As a section of the dashboard's side panel (section) its title folds it and its list scrolls inside it.
 */
export function LatestCommits({
  counted,
  work,
  show,
  activity,
  count,
  section,
}: {
  /** the newest commits of the work that counts, as the dashboard read them */
  counted: Commit[]
  work: Work
  /** the reader's own choice of kinds (the cookie), if any */
  show?: Work
  activity: Schemas["Activity"]
  count: number
  /** its section of the dashboard's side panel, if it is one */
  section?: Section
}) {
  const s = useShow(show, work)
  return (
    <Card aria-labelledby="latest" className={cn("flex flex-col gap-3", sectionCard(section))}>
      <div className="flex flex-col gap-1.5">
        <div className="flex items-center gap-2">
          <SectionTitle id="latest">Latest commits</SectionTitle>
          <SectionFold id="latest" section={section} />
        </div>
        {section?.open !== false && (
          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-sm text-muted-foreground">
            <ShowPicker
              shown={s.shown}
              follows={s.follows}
              activity={activity}
              onChange={s.set}
              align="start"
              className="-ml-1.5"
            />
            <Link href="/commits" className="hover:text-primary hover:underline">
              All commits
            </Link>
          </div>
        )}
      </div>
      {section?.open !== false && (
        <div id="latest-body" className={cn("flex flex-col gap-3", sectionBody(section))}>
          <Feed key={s.shown.join(",")} counted={sameWork(s.shown, work) ? counted.slice(0, count) : undefined} shown={s.shown} />
        </div>
      )}
    </Card>
  )
}

/** Feed is the list of one choice of kinds: the dashboard's commits when it is the work that counts, else read page by page. */
function Feed({ counted, shown }: { counted?: Commit[]; shown: Work }) {
  const own = counted === undefined
  const end = useRef<HTMLDivElement>(null)
  const more = useMore(moreQuery(api, shown), counted ?? [], (p) => p.commits, commitKey, end, true, own)
  return (
    <>
      {own && !more.loaded ? <p className="text-sm text-muted-foreground">Loading…</p> : <CommitList commits={more.items} />}
      <div ref={end} aria-live="polite" className="text-center text-xs text-muted-foreground">
        {more.loading ? "Loading more…" : null}
        {more.done && !more.loading ? (
          <Link href="/commits" className="hover:text-primary hover:underline">
            Every commit, with filters
          </Link>
        ) : null}
      </div>
    </>
  )
}
