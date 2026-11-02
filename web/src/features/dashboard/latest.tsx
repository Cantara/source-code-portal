"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useState } from "react"
import { Card, CardTitle } from "@/components/ui/card"
import { api, type Commit } from "@/lib/api/client"
import { CommitList } from "@/entities/commit/commit-list"
import { latestAllQuery } from "./api"

/**
 * LatestCommits are the newest commits by people (the dashboard's), and on request the newest of all, automation and sweeps
 * included.
 */
export function LatestCommits({ people, count }: { people: Commit[]; count: number }) {
  const [all, setAll] = useState(false)
  const { data } = useQuery({ ...latestAllQuery(api, count), enabled: all })
  const commits = all ? (data?.commits ?? []) : people.slice(0, count)
  return (
    <Card aria-labelledby="latest" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="latest">{all ? "Latest commits" : "Latest commits by people"}</CardTitle>
        <div className="flex items-center gap-4 text-sm">
          <label className="flex items-center gap-2 text-muted-foreground">
            <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} className="accent-primary" />
            Include automation
          </label>
          <Link href="/commits" className="hover:text-primary hover:underline">
            All commits
          </Link>
        </div>
      </div>
      {all && !data ? <p className="text-sm text-muted-foreground">Loading…</p> : <CommitList commits={commits} />}
    </Card>
  )
}
