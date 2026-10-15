"use client"

import { useQuery, useQueryClient } from "@tanstack/react-query"
import Link from "next/link"
import { Card, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api/client"
import { repoCommitsPath } from "@/lib/paths"
import { CommitList } from "@/entities/commit/commit-list"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { ProblemAlert } from "@/shared/problem-alert"
import { historyQuery } from "./history"

/** latestSize is how many of the newest commits of the history the repository page shows. */
export const latestSize = 30

/** LatestCommits is the newest commits of a repository's history with their part of the git graph, and the way to the navigator. */
export function LatestCommits({ owner, name }: { owner: string; name: string }) {
  const qc = useQueryClient()
  const { data, error } = useQuery(historyQuery(api, qc, owner, name, {}, latestSize, { page: 1 }))
  return (
    <Card aria-labelledby="latest">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="latest">Latest commits</CardTitle>
        {data && <FreshnessBadge freshness={data.freshness} />}
      </div>
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <CommitList
          commits={data.commits}
          showRepo={false}
          graph={{ rows: data.commits.map((c) => c.graph), lanes: data.lanes }}
        />
      )}
      <Link href={repoCommitsPath(owner, name)} className="mt-2 inline-block text-sm hover:underline">
        {data && data.page.total > data.commits.length ? `All ${data.page.total} commits` : "Commit log"}
      </Link>
    </Card>
  )
}
