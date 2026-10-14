import { groupQuery } from "@/lib/api/queries"
import { load, peek, prefetch } from "@/lib/load"
import type { Route } from "@/lib/page"
import { groupPath } from "@/lib/paths"
import { groupCommitsQuery } from "./api"
import { GroupView } from "./view"

type Params = { group: string }

/** groupRoute is `/groups/{group}`: the group with its repositories and its latest commits. */
export const groupRoute: Route<Params> = {
  load: ({ api, qc, params: { group } }) =>
    Promise.all([load(qc, groupQuery(api, group)), prefetch(qc, groupCommitsQuery(api, group))]),
  async metadata({ api, qc, params: { group } }) {
    const g = await peek(qc, groupQuery(api, group))
    return {
      title: g?.name ?? group,
      description: g?.description || `The repositories of the group ${g?.name ?? group}.`,
      alternates: { canonical: groupPath(g?.id ?? group) },
    }
  },
  render: ({ params: { group } }) => <GroupView group={group} />,
}
