"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useState, type ReactNode } from "react"
import { Card, CardTitle } from "@/components/ui/card"
import { api, type Commit } from "@/lib/api/client"
import { CommitList } from "@/entities/commit/commit-list"
import { by, has, type Work } from "@/lib/preferences"
import { latestAllQuery } from "./api"

/**
 * LatestCommits are the newest commits of the work that counts (the dashboard's, through the reader's lens), and, when the lens leaves
 * automation out, on request the newest of every kind. As the dashboard's side panel it carries the button that folds it.
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
  const { data } = useQuery({ ...latestAllQuery(api, count), enabled: all })
  const commits = all ? (data?.commits ?? []) : counted.slice(0, count)
  return (
    <Card aria-labelledby="latest" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="latest">{all ? "Latest commits" : `Latest commits ${by(work)}`}</CardTitle>
        <div className="flex items-center gap-4 text-sm">
          {!has(work, "automation") && (
            <label className="flex items-center gap-2 text-muted-foreground">
              <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} className="accent-primary" />
              Include automation
            </label>
          )}
          <Link href="/commits" className="hover:text-primary hover:underline">
            All commits
          </Link>
          {fold}
        </div>
      </div>
      {all && !data ? <p className="text-sm text-muted-foreground">Loading…</p> : <CommitList commits={commits} />}
    </Card>
  )
}
