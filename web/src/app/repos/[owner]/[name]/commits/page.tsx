import { HydrationBoundary } from "@tanstack/react-query"
import type { Metadata } from "next"
import { commitLogQuery } from "@/features/commits/api"
import { RepoCommitsView } from "@/features/commits/view"
import { serverApi } from "@/lib/api/server"
import { serverQueryClient, state } from "@/lib/load"
import { repoCommitsPath } from "@/lib/paths"
import { loadRepo } from "../repo"

type Props = { params: Promise<{ owner: string; name: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { owner, name } = await params
  return {
    title: `Commits in ${owner}/${name}`,
    description: `The latest commits on the default branch of ${owner}/${name}.`,
    alternates: { canonical: repoCommitsPath(owner, name) },
  }
}

export default async function RepoCommitsPage({ params }: Props) {
  const { owner, name } = await params
  const api = await serverApi()
  const qc = serverQueryClient()
  await Promise.all([
    loadRepo(api, owner, name, "/commits"),
    qc.prefetchInfiniteQuery(commitLogQuery(api, qc, { repo: `${owner}/${name}` })),
  ])
  return (
    <HydrationBoundary state={state()}>
      <RepoCommitsView owner={owner} name={name} />
    </HydrationBoundary>
  )
}
