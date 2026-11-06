"use client"

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api/client"
import { pullsQuery } from "@/lib/api/queries"
import { RepoQueue } from "@/shared/repo-queue"
import { PullList, pullStates } from "@/entities/pull/pull"
import { repoPulls } from "./api"

/**
 * RepoPulls is a repository's open pull requests whose turn it is first: the counts by state, the ten that wait longest, and the
 * ways to all of them on /pulls and on GitHub. Nothing to show when none is open but a line saying so.
 */
export function RepoPulls({ owner, name }: { owner: string; name: string }) {
  const { data, error } = useQuery(pullsQuery(api, repoPulls(owner, name)))
  return (
    <RepoQueue
      id="pulls"
      title="Pull requests"
      data={data}
      error={error}
      states={pullStates}
      list={data && <PullList pulls={data.pulls} showRepo={false} compact className="mt-2" />}
      all={`/pulls?repo=${encodeURIComponent(`${owner}/${name}`.toLowerCase())}&drafts=true`}
      github={data?.pulls[0]?.url.replace(/\/pull\/\d+$/, "/pulls")}
    />
  )
}
