import { PanelBoundary } from "@/shared/panel-boundary"
import { docListQuery, docQuery, repoQuery } from "@/lib/api/queries"
import { loadRepo, peek, prefetch } from "@/lib/load"
import type { Panel, Route } from "@/lib/page"
import { repoPath } from "@/lib/paths"
import { readmeOf } from "./readme"
import { RepoView } from "./view"

type Params = { owner: string; name: string }

/** repoRoute is `/repos/{owner}/{name}` with the panels app/ composes into it, at the repository's current address. */
export function repoRoute(panels: Panel<Params>[]): Route<Params> {
  const column = (c: Panel<Params>["column"], params: Params) =>
    panels.filter((p) => p.column === c).map((p) => <PanelBoundary key={p.id}>{p.render(params)}</PanelBoundary>)
  return {
    async load(ctx) {
      const { api, qc, params } = ctx
      const { owner, name } = params
      await loadRepo(api, qc, owner, name)
      const [docs] = await Promise.all([peek(qc, docListQuery(api, owner, name)), ...panels.map((p) => p.prefetch(ctx))])
      const readme = readmeOf(docs)
      if (readme) await prefetch(qc, docQuery(api, owner, name, readme.id))
    },
    async metadata({ api, qc, params: { owner, name } }) {
      const data = await peek(qc, repoQuery(api, owner, name))
      const repo = data?.repo
      return {
        title: data?.title || `${owner}/${name}`,
        description: data?.descriptor.description || repo?.description || `The repository ${owner}/${name}.`,
        alternates: { canonical: repo ? repoPath(repo.owner, repo.name) : repoPath(owner, name) },
      }
    },
    render: ({ params }) => (
      <RepoView owner={params.owner} name={params.name} main={column("main", params)} side={column("side", params)} />
    ),
  }
}
