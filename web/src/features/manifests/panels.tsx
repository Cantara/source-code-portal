import { prefetch } from "@/lib/load"
import type { Panel } from "@/lib/page"
import { manifestsQuery } from "./api"
import { Dependencies, TechnologyCard } from "./view"

type Params = { owner: string; name: string }

const prefetchManifests: Panel<Params>["prefetch"] = ({ api, qc, params: { owner, name } }) =>
  prefetch(qc, manifestsQuery(api, owner, name))

/** technologyPanel is the repository's technology and the catalogued repositories it uses and is used by. */
export const technologyPanel: Panel<Params> = {
  id: "technology",
  column: "side",
  prefetch: prefetchManifests,
  render: ({ owner, name }) => <TechnologyCard owner={owner} name={name} />,
}

/** dependenciesPanel is the repository's declared dependencies per manifest. */
export const dependenciesPanel: Panel<Params> = {
  id: "dependencies",
  column: "main",
  prefetch: prefetchManifests,
  render: ({ owner, name }) => <Dependencies owner={owner} name={name} />,
}
