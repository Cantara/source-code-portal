import { HydrationBoundary } from "@tanstack/react-query"
import type { Metadata } from "next"
import { docListQuery, docQuery, manifestsQuery } from "@/features/repos/api"
import { RepoView } from "@/features/repos/view"
import { serverApi } from "@/lib/api/server"
import { prefetch, serverQueryClient, state } from "@/lib/load"
import { repoPath } from "@/lib/paths"
import { loadRepo, peekRepo } from "./repo"

type Props = { params: Promise<{ owner: string; name: string }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { owner, name } = await params
  const data = await peekRepo(await serverApi(), owner, name)
  const repo = data?.repo
  return {
    title: data?.title || `${owner}/${name}`,
    description: data?.descriptor.description || repo?.description || `The repository ${owner}/${name}.`,
    alternates: { canonical: repo ? repoPath(repo.owner, repo.name) : repoPath(owner, name) },
  }
}

export default async function RepoPage({ params }: Props) {
  const { owner, name } = await params
  const api = await serverApi()
  const [, docs] = await Promise.all([
    loadRepo(api, owner, name),
    serverQueryClient()
      .fetchQuery(docListQuery(api, owner, name))
      .catch(() => undefined),
    prefetch(manifestsQuery(api, owner, name)),
  ])
  const readme = docs?.docs.find((d) => d.readme)
  if (readme) await prefetch(docQuery(api, owner, name, readme.id))
  return (
    <HydrationBoundary state={state()}>
      <RepoView owner={owner} name={name} />
    </HydrationBoundary>
  )
}
