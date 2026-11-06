"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { Card, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api/client"
import { releasesQuery } from "@/lib/api/queries"
import { count } from "@/lib/format"
import { ProblemAlert } from "@/shared/problem-alert"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { ReleaseList } from "@/entities/release/release"
import { releasesPath, repoReleases } from "./api"

/**
 * RepoReleases is a repository's recent releases, the newest first (the ten newest of the 20 the portal holds), and the way to all of
 * them on /releases. A line saying so when it has published none.
 */
export function RepoReleases({ owner, name }: { owner: string; name: string }) {
  const address = `${owner}/${name}`.toLowerCase()
  const { data, error } = useQuery(releasesQuery(api, repoReleases(owner, name)))
  return (
    <Card aria-labelledby="releases">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="releases">Releases</CardTitle>
        {data && <FreshnessBadge freshness={data.freshness} />}
      </div>
      {error && !data && <ProblemAlert error={error} />}
      {data && data.counts.all === 0 && <p className="mt-1 text-sm text-muted-foreground">None published.</p>}
      {data && data.counts.all > 0 && (
        <>
          <ReleaseList releases={data.releases} showRepo={false} className="mt-2" />
          <Link href={releasesPath({ repo: address, variant: "all" })} className="mt-2 inline-block text-sm hover:underline">
            All {count(data.counts.all)} held
          </Link>
        </>
      )}
    </Card>
  )
}
