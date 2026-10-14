import { HydrationBoundary } from "@tanstack/react-query"
import type { Metadata } from "next"
import { commitFilter, commitLogQuery, groupsQuery } from "@/features/commits/api"
import { AllCommitsView } from "@/features/commits/view"
import { serverApi } from "@/lib/api/server"
import { prefetch, serverQueryClient, state } from "@/lib/load"

export const metadata: Metadata = {
  title: "Commits",
  description: "The latest commits across the repositories of the organisation.",
  alternates: { canonical: "/commits" },
}

type Props = { searchParams: Promise<Record<string, string | string[] | undefined>> }

export default async function CommitsPage({ searchParams }: Props) {
  const api = await serverApi()
  const qc = serverQueryClient()
  await Promise.all([
    qc.prefetchInfiniteQuery(commitLogQuery(api, qc, commitFilter(await searchParams))),
    prefetch(groupsQuery(api)),
  ])
  return (
    <HydrationBoundary state={state()}>
      <AllCommitsView />
    </HydrationBoundary>
  )
}
