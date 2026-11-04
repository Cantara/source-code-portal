import { prefetch } from "@/lib/load"
import type { Panel } from "@/lib/page"
import { repoQuery } from "@/lib/api/queries"
import { PackagesCard } from "./view"

type Params = { owner: string; name: string }

/** packagesPanel is what the repository publishes to package registries, from its summary. */
export const packagesPanel: Panel<Params> = {
  id: "packages",
  column: "side",
  prefetch: ({ api, qc, params: { owner, name } }) => prefetch(qc, repoQuery(api, owner, name)),
  render: ({ owner, name }) => <PackagesCard owner={owner} name={name} />,
}
