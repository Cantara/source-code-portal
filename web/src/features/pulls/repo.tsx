"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { Card, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api/client"
import { pullsQuery } from "@/lib/api/queries"
import { count, plural } from "@/lib/format"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { ProblemAlert } from "@/shared/problem-alert"
import { PullList, pullStates } from "@/entities/pull/pull"
import { repoPulls } from "./api"

/**
 * RepoPulls is a repository's open pull requests whose turn it is first: the counts by state, the ten that wait longest, and the
 * ways to all of them on /pulls and on GitHub. Nothing to show when none is open but a line saying so.
 */
export function RepoPulls({ owner, name }: { owner: string; name: string }) {
  const { data, error } = useQuery(pullsQuery(api, repoPulls(owner, name)))
  return (
    <Card aria-labelledby="pulls">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="pulls">Pull requests</CardTitle>
        {data && <FreshnessBadge freshness={data.freshness} />}
      </div>
      {error && !data && <ProblemAlert error={error} />}
      {data && data.counts.open === 0 && <p className="mt-1 text-sm text-muted-foreground">None open.</p>}
      {data && data.counts.open > 0 && (
        <>
          <p className="mt-1 text-sm text-muted-foreground">
            {plural(data.counts.open, "open", "open")}:{" "}
            {pullStates
              .filter((s) => data.counts.states[s.state] > 0)
              .map((s) => `${s.label} ${count(data.counts.states[s.state])}`)
              .join(" · ")}
          </p>
          <PullList pulls={data.pulls} showRepo={false} compact className="mt-2" />
          <div className="mt-2 flex flex-wrap gap-x-4 text-sm">
            <Link
              href={`/pulls?repo=${encodeURIComponent(`${owner}/${name}`.toLowerCase())}&drafts=true`}
              className="hover:underline"
            >
              All {count(data.counts.open)} open
            </Link>
            {data.pulls[0] && (
              <a
                href={data.pulls[0].url.replace(/\/pull\/\d+$/, "/pulls")}
                rel="noreferrer"
                className="text-muted-foreground hover:underline"
              >
                On GitHub
              </a>
            )}
          </div>
        </>
      )}
    </Card>
  )
}
