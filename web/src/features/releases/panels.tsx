import { releasesQuery } from "@/lib/api/queries"
import { prefetch } from "@/lib/load"
import type { Panel } from "@/lib/page"
import { repoReleases } from "./api"
import { RepoReleases } from "./repo"

type Params = { owner: string; name: string }

/** repoReleasesPanel is the repository's recent releases, the newest first. */
export const repoReleasesPanel: Panel<Params> = {
  id: "releases",
  column: "side",
  prefetch: ({ api, qc, params: { owner, name } }) => prefetch(qc, releasesQuery(api, repoReleases(owner, name))),
  render: ({ owner, name }) => <RepoReleases owner={owner} name={name} />,
}
