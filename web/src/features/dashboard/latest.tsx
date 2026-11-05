"use client"

import { useInfiniteQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useEffect, useRef, useState, type ReactNode } from "react"
import { Card, CardTitle } from "@/components/ui/card"
import { api, type Commit } from "@/lib/api/client"
import { CommitList } from "@/entities/commit/commit-list"
import { every, has, type Work } from "@/lib/preferences"
import { moreQuery, morePages } from "./api"

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
 * LatestCommits are the newest commits of the work that counts (the dashboard's, through the reader's lens), and, when the lens leaves
 * automation out, on request the newest of every kind. Scrolled to its end, the list reads the next page of the same commits and
 * goes on, up to morePages pages; past them "All commits" has the rest. As the dashboard's side panel it carries the button that
 * folds it.
 */
export function LatestCommits({
  counted,
  work,
  count,
  fold,
}: {
  counted: Commit[]
  work: Work
  count: number
  /** The side panel's button that folds it away, at the header's end. */
  fold?: ReactNode
}) {
  const [all, setAll] = useState(false)
  const [more, setMore] = useState(false)
  const lens = all ? every : work
  const q = useInfiniteQuery({ ...moreQuery(api, lens), enabled: all || more })
  const read = q.data?.pages.flatMap((p) => p.commits) ?? []
  const commits = dedupe(all ? read : [...counted.slice(0, count), ...(more ? read : [])])
  const end = useRef<HTMLDivElement>(null)
  const visible = useRef(false)
  const next = useRef(() => {})
  const pages = q.data?.pages.length ?? 0
  useEffect(() => {
    next.current = () => {
      if (!more && !all) setMore(true)
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
  const done = (more || all) && q.isSuccess && !q.hasNextPage
  return (
    <Card aria-labelledby="latest" className="flex flex-col gap-3">
      <div className="flex flex-col gap-1.5">
        <div className="flex items-center gap-2">
          <CardTitle id="latest" className="min-w-0 flex-1">
            Latest commits
          </CardTitle>
          {fold}
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
          {!has(work, "automation") && (
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} className="accent-primary" />
              Include automation
            </label>
          )}
          <Link href="/commits" className="hover:text-primary hover:underline">
            All commits
          </Link>
        </div>
      </div>
      {all && !q.data ? <p className="text-sm text-muted-foreground">Loading…</p> : <CommitList commits={commits} />}
      <div ref={end} aria-live="polite" className="text-center text-xs text-muted-foreground">
        {q.isFetching && (more || all) && q.data ? "Loading more…" : null}
        {done || (pages >= morePages && !q.isFetching) ? (
          <Link href="/commits" className="hover:text-primary hover:underline">
            Every commit, with filters
          </Link>
        ) : null}
      </div>
    </Card>
  )
}
