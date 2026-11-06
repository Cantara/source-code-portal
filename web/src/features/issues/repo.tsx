"use client"

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api/client"
import { issuesQuery } from "@/lib/api/queries"
import { RepoQueue } from "@/shared/repo-queue"
import { IssueList, issueStates } from "@/entities/issue/issue"
import { issuesPath, repoIssues } from "./api"

/**
 * RepoIssues is a repository's open issues, what waits first: the counts by state, the ten that wait longest, and the ways to all
 * of them on /issues and on GitHub. A line saying so when none is open.
 */
export function RepoIssues({ owner, name }: { owner: string; name: string }) {
  const { data, error } = useQuery(issuesQuery(api, repoIssues(owner, name)))
  return (
    <RepoQueue
      id="issues"
      title="Issues"
      data={data}
      error={error}
      states={issueStates}
      list={data && <IssueList issues={data.issues} showRepo={false} compact className="mt-2" />}
      all={issuesPath({ repo: `${owner}/${name}` })}
      github={data?.issues[0]?.url.replace(/\/issues\/\d+$/, "/issues")}
    />
  )
}
