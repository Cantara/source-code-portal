import { HydrationBoundary } from "@tanstack/react-query"
import type { Metadata } from "next"
import { docListQuery, docQuery } from "@/features/docs/api"
import { DocView } from "@/features/docs/view"
import { serverApi } from "@/lib/api/server"
import { load, peek, prefetch, state } from "@/lib/load"
import { docPath } from "@/lib/paths"

type Props = { params: Promise<{ owner: string; name: string; path: string[] }> }

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { owner, name, path } = await params
  const doc = await peek(docQuery(await serverApi(), owner, name, path.join("/")))
  return {
    title: doc?.title || path.join("/"),
    description: doc?.description || `${doc?.path ?? path.join("/")} in ${owner}/${name}.`,
    alternates: { canonical: docPath(owner, name, doc?.path ?? path.join("/")) },
  }
}

export default async function DocPage({ params }: Props) {
  const { owner, name, path } = await params
  const p = path.join("/")
  const api = await serverApi()
  await Promise.all([load(docQuery(api, owner, name, p)), prefetch(docListQuery(api, owner, name))])
  return (
    <HydrationBoundary state={state()}>
      <DocView owner={owner} name={name} path={p} />
    </HydrationBoundary>
  )
}
