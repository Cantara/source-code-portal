"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { Card, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api/client"
import { issuesQuery } from "@/lib/api/queries"
import { count, plural } from "@/lib/format"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { ProblemAlert } from "@/shared/problem-alert"
import { IssueList, issueStates } from "@/entities/issue/issue"
import { issuesPath, repoIssues } from "./api"

/**
 * RepoIssues is a repository's open issues, what waits first: the counts by state, the ten that wait longest, and the ways to all
 * of them on /issues and on GitHub. A line saying so when none is open.
 */
export function RepoIssues({ owner, name }: { owner: string; name: string }) {
  const { data, error } = useQuery(issuesQuery(api, repoIssues(owner, name)))
  return (
    <Card aria-labelledby="issues">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="issues">Issues</CardTitle>
        {data && <FreshnessBadge freshness={data.freshness} />}
      </div>
      {error && !data && <ProblemAlert error={error} />}
      {data && data.counts.open === 0 && <p className="mt-1 text-sm text-muted-foreground">None open.</p>}
      {data && data.counts.open > 0 && (
        <>
          <p className="mt-1 text-sm text-muted-foreground">
            {plural(data.counts.open, "open", "open")}:{" "}
            {issueStates
              .filter((s) => data.counts.states[s.state] > 0)
              .map((s) => `${s.label} ${count(data.counts.states[s.state])}`)
              .join(" · ")}
          </p>
          <IssueList issues={data.issues} showRepo={false} compact className="mt-2" />
          <div className="mt-2 flex flex-wrap gap-x-4 text-sm">
            <Link href={issuesPath({ repo: `${owner}/${name}` })} className="hover:underline">
              All {count(data.counts.open)} open
            </Link>
            {data.issues[0] && (
              <a
                href={data.issues[0].url.replace(/\/issues\/\d+$/, "/issues")}
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
