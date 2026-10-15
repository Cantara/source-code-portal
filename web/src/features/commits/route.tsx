import { groupQuery, groupsQuery, repoQuery } from "@/lib/api/queries"
import { load, loadRepo, peek, prefetch } from "@/lib/load"
import type { Ctx, Route } from "@/lib/page"
import { pageSize, sizeCookie } from "@/lib/paging"
import { groupCommitsPath, repoCommitsPath } from "@/lib/paths"
import { commitLogQuery, defaultLogSize, logState } from "./api"
import { defaultHistorySize, historyQuery, historyState, request } from "./history"
import { NavigatorView } from "./navigator"
import { AllCommitsView, GroupCommitsView } from "./view"
import { crumbs } from "@/lib/crumbs"
import { ld } from "@/lib/seo"

/** logSize and historySize are the reader's page sizes: the one they chose last, else the default. */
const logSize = (ctx: Ctx<unknown>) => pageSize(ctx.cookies[sizeCookie("commits")], defaultLogSize)
const historySize = (ctx: Ctx<unknown>) => pageSize(ctx.cookies[sizeCookie("history")], defaultHistorySize)

/** allCommitsRoute is `/commits`: the commit log across repositories, filtered through the query string. */
export const allCommitsRoute: Route<object> = {
  load: (ctx) =>
    Promise.all([
      ctx.qc.query(commitLogQuery(ctx.api, ctx.qc, logState(ctx.search, logSize(ctx)))).catch(() => undefined),
      prefetch(ctx.qc, groupsQuery(ctx.api)),
    ]),
  metadata: () => ({
    title: "Commits",
    description: "The latest commits across the repositories of the organisation.",
    alternates: { canonical: "/commits" },
  }),
  structured: (_, base) => [ld.page(base, "/commits", "Commits"), ld.breadcrumbs(base, crumbs.commits())],
  render: (ctx) => <AllCommitsView defaultSize={logSize(ctx)} />,
}

/** groupCommitsRoute is `/groups/{group}/commits`. */
export const groupCommitsRoute: Route<{ group: string }> = {
  load: (ctx) => {
    const { api, qc, params, search } = ctx
    const { page, size } = logState(search, logSize(ctx))
    return Promise.all([
      load(qc, groupQuery(api, params.group)),
      qc.query(commitLogQuery(api, qc, { filter: { group: params.group }, page, size })).catch(() => undefined),
    ])
  },
  async metadata({ api, qc, params: { group } }) {
    const g = await peek(qc, groupQuery(api, group))
    const named = g?.name ?? group
    return {
      title: `Commits in ${named}`,
      description: `The latest commits in the repositories of the group ${named}.`,
      alternates: { canonical: groupCommitsPath(g?.id ?? group) },
    }
  },
  async structured({ api, qc, params: { group } }, base) {
    const g = await peek(qc, groupQuery(api, group))
    if (!g) return []
    return [
      ld.page(base, groupCommitsPath(g.id), `Commits in ${g.name}`),
      ld.breadcrumbs(base, crumbs.groupCommits(g.id, g.name)),
    ]
  },
  render: (ctx) => <GroupCommitsView group={ctx.params.group} defaultSize={logSize(ctx)} />,
}

type RepoParams = { owner: string; name: string }

/** repoCommitsRoute is `/repos/{owner}/{name}/commits`, the navigator of the repository's history at its current address. */
export const repoCommitsRoute: Route<RepoParams> = {
  load: (ctx) => {
    const {
      api,
      qc,
      params: { owner, name },
      search,
    } = ctx
    const st = historyState(search, historySize(ctx))
    return Promise.all([
      loadRepo(api, qc, owner, name, repoCommitsPath),
      qc.query(historyQuery(api, qc, owner, name, st.filter, st.size, request(st))).catch(() => undefined),
    ])
  },
  async metadata({ api, qc, params: { owner, name } }) {
    const r = (await peek(qc, repoQuery(api, owner, name)))?.repo
    const o = r?.owner ?? owner
    const n = r?.name ?? name
    return {
      title: `Commits in ${o}/${n}`,
      description: `The history of ${o}/${n} as a git graph: its branches, tags and commits.`,
      alternates: { canonical: repoCommitsPath(o, n) },
    }
  },
  async structured({ api, qc, params: { owner, name } }, base) {
    const d = await peek(qc, repoQuery(api, owner, name))
    if (!d) return []
    const r = d.repo
    return [
      ld.page(base, repoCommitsPath(r.owner, r.name), `Commits in ${r.owner}/${r.name}`),
      ld.breadcrumbs(base, crumbs.repoCommits(r.owner, r.name, d.group)),
    ]
  },
  render: (ctx) => <NavigatorView owner={ctx.params.owner} name={ctx.params.name} defaultSize={historySize(ctx)} />,
}
