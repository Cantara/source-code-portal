import { issuesQuery } from "@/lib/api/queries"
import { prefetch } from "@/lib/load"
import type { Panel } from "@/lib/page"
import { Switched } from "@/lib/rules-context"
import { repoIssues } from "./api"
import { RepoIssues } from "./repo"

type Params = { owner: string; name: string }

/** repoIssuesPanel is the repository's open issues, what waits first. */
export const repoIssuesPanel: Panel<Params> = {
  id: "issues",
  column: "side",
  prefetch: ({ api, qc, params: { owner, name } }) => prefetch(qc, issuesQuery(api, repoIssues(owner, name))),
  render: ({ owner, name }) => (
    <Switched feature="issues">
      <RepoIssues owner={owner} name={name} />
    </Switched>
  ),
}
