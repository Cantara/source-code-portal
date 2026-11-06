import { pullsQuery } from "@/lib/api/queries"
import { prefetch } from "@/lib/load"
import type { Panel } from "@/lib/page"
import { Switched } from "@/lib/rules-context"
import { repoPulls } from "./api"
import { RepoPulls } from "./repo"

type Params = { owner: string; name: string }

/** repoPullsPanel is the repository's open pull requests, whose turn first. */
export const repoPullsPanel: Panel<Params> = {
  id: "pulls",
  column: "side",
  prefetch: ({ api, qc, params: { owner, name } }) => prefetch(qc, pullsQuery(api, repoPulls(owner, name))),
  render: ({ owner, name }) => (
    <Switched feature="pulls">
      <RepoPulls owner={owner} name={name} />
    </Switched>
  ),
}
