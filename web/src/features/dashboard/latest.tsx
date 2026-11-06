"use client"

import { useInfiniteQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useEffect, useRef, useState } from "react"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/cn"
import { api, type Commit, type Schemas } from "@/lib/api/client"
import { CommitList } from "@/entities/commit/commit-list"
import { sameWork, type Work } from "@/lib/preferences"
import { moreQuery, morePages } from "./api"
import { ShowPicker, useShow } from "./show"
import { sectionBody, sectionCard, SectionFold, SectionTitle, type Section } from "./side"

/** dedupe keeps each commit once, the first time it comes: new commits move older ones to later pages while the reader scrolls. */
export function dedupe(commits: Commit[]): Commit[] {
  const seen = new Set<string>()
  return commits.filter((c) => {
    const k = `${c.repo}@${c.sha}`
    if (seen.has(k)) return false
    seen.add(k)
    return true
  })
}

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
  const [more, setMore] = useState(own)
  const q = useInfiniteQuery({ ...moreQuery(api, shown), enabled: more })
  const read = more ? (q.data?.pages.flatMap((p) => p.commits) ?? []) : []
  const commits = dedupe([...(counted ?? []), ...read])
  const end = useRef<HTMLDivElement>(null)
  const visible = useRef(false)
  const next = useRef(() => {})
  const pages = q.data?.pages.length ?? 0
  useEffect(() => {
    next.current = () => {
      if (!more) setMore(true)
      else if (q.hasNextPage && !q.isFetching && pages < morePages) void q.fetchNextPage()
    }
  })
  // The end of the list in view (in the panel's own scroll on a wide screen, the page's on a phone) reads the next page.
  useEffect(() => {
    const el = end.current
    if (!el || typeof IntersectionObserver === "undefined") return // without it the list stays, "All commits" has the rest
    const seen = new IntersectionObserver(
      (es) => {
        visible.current = es.some((e) => e.isIntersecting)
        if (visible.current) next.current()
      },
      { rootMargin: "200px" },
    )
    seen.observe(el)
    return () => seen.disconnect()
  }, [])
  // A page that left the end in view (a tall panel, a short page) reads the next at once.
  useEffect(() => {
    if (!q.isFetching && visible.current) next.current()
  }, [q.isFetching, pages])
  const done = more && q.isSuccess && !q.hasNextPage
  return (
    <>
      {own && !q.data ? <p className="text-sm text-muted-foreground">Loading…</p> : <CommitList commits={commits} />}
      <div ref={end} aria-live="polite" className="text-center text-xs text-muted-foreground">
        {q.isFetching && more && q.data ? "Loading more…" : null}
        {done || (pages >= morePages && !q.isFetching) ? (
          <Link href="/commits" className="hover:text-primary hover:underline">
            Every commit, with filters
          </Link>
        ) : null}
      </div>
    </>
  )
}
