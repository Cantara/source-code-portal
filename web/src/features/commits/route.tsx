import { groupQuery, groupsQuery, repoQuery } from "@/lib/api/queries"
import { load, loadRepo, peek, prefetch } from "@/lib/load"
import type { Route } from "@/lib/page"
import { groupCommitsPath, repoCommitsPath } from "@/lib/paths"
import { commitFilter, commitLogQuery } from "./api"
import { AllCommitsView, GroupCommitsView, RepoCommitsView } from "./view"

/** allCommitsRoute is `/commits`: the commit log across repositories, filtered through the query string. */
export const allCommitsRoute: Route<object> = {
  load: ({ api, qc, search }) =>
    Promise.all([
      qc.infiniteQuery(commitLogQuery(api, qc, commitFilter(search))).catch(() => undefined),
      prefetch(qc, groupsQuery(api)),
    ]),
  metadata: () => ({
    title: "Commits",
    description: "The latest commits across the repositories of the organisation.",
    alternates: { canonical: "/commits" },
  }),
  render: () => <AllCommitsView />,
}

/** groupCommitsRoute is `/groups/{group}/commits`. */
export const groupCommitsRoute: Route<{ group: string }> = {
  load: ({ api, qc, params: { group } }) =>
    Promise.all([load(qc, groupQuery(api, group)), qc.infiniteQuery(commitLogQuery(api, qc, { group })).catch(() => undefined)]),
  async metadata({ api, qc, params: { group } }) {
    const g = await peek(qc, groupQuery(api, group))
    const named = g?.name ?? group
    return {
      title: `Commits in ${named}`,
      description: `The latest commits in the repositories of the group ${named}.`,
      alternates: { canonical: groupCommitsPath(g?.id ?? group) },
    }
  },
  render: ({ params: { group } }) => <GroupCommitsView group={group} />,
}

type RepoParams = { owner: string; name: string }

/** repoCommitsRoute is `/repos/{owner}/{name}/commits`, at the repository's current address. */
export const repoCommitsRoute: Route<RepoParams> = {
  load: ({ api, qc, params: { owner, name } }) =>
    Promise.all([
      loadRepo(api, qc, owner, name, repoCommitsPath),
      qc.infiniteQuery(commitLogQuery(api, qc, { repo: `${owner}/${name}` })).catch(() => undefined),
    ]),
  async metadata({ api, qc, params: { owner, name } }) {
    const r = (await peek(qc, repoQuery(api, owner, name)))?.repo
    const o = r?.owner ?? owner
    const n = r?.name ?? name
    return {
      title: `Commits in ${o}/${n}`,
      description: `The latest commits on the default branch of ${o}/${n}.`,
      alternates: { canonical: repoCommitsPath(o, n) },
    }
  },
  render: ({ params: { owner, name } }) => <RepoCommitsView owner={owner} name={name} />,
}
