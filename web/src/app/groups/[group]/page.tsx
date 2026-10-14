import { HydrationBoundary } from "@tanstack/react-query"
import type { Metadata } from "next"
import { groupCommitsQuery, groupQuery } from "@/features/groups/api"
import { GroupView } from "@/features/groups/view"
import { serverApi } from "@/lib/api/server"
import { load, peek, prefetch, state } from "@/lib/load"
import { groupPath } from "@/lib/paths"

type Props = { params: Promise<{ group: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { group } = await params
  const g = await peek(groupQuery(await serverApi(), group))
  return {
    title: g?.name ?? group,
    description: g?.description || `The repositories of the group ${g?.name ?? group}.`,
    alternates: { canonical: groupPath(g?.id ?? group) },
  }
}

export default async function GroupPage({ params }: Props) {
  const { group } = await params
  const api = await serverApi()
  await Promise.all([load(groupQuery(api, group)), prefetch(groupCommitsQuery(api, group))])
  return (
    <HydrationBoundary state={state()}>
      <GroupView group={group} />
    </HydrationBoundary>
  )
}
