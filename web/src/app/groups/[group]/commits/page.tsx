import { HydrationBoundary } from "@tanstack/react-query"
import type { Metadata } from "next"
import { commitLogQuery } from "@/features/commits/api"
import { GroupCommitsView } from "@/features/commits/view"
import { groupQuery } from "@/features/groups/api"
import { serverApi } from "@/lib/api/server"
import { load, peek, serverQueryClient, state } from "@/lib/load"
import { groupCommitsPath } from "@/lib/paths"

type Props = { params: Promise<{ group: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { group } = await params
  const g = await peek(groupQuery(await serverApi(), group))
  return {
    title: `Commits in ${group}`,
    description: `The latest commits in the repositories of the group ${g?.name ?? group}.`,
    alternates: { canonical: groupCommitsPath(g?.id ?? group) },
  }
}

export default async function GroupCommitsPage({ params }: Props) {
  const { group } = await params
  const api = await serverApi()
  const qc = serverQueryClient()
  await Promise.all([load(groupQuery(api, group)), qc.prefetchInfiniteQuery(commitLogQuery(api, qc, { group }))])
  return (
    <HydrationBoundary state={state()}>
      <GroupCommitsView group={group} />
    </HydrationBoundary>
  )
}
