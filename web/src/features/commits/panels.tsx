import { prefetch } from "@/lib/load"
import type { Panel } from "@/lib/page"
import { historyQuery } from "./history"
import { LatestCommits, latestSize } from "./latest"

type Params = { owner: string; name: string }

/** latestCommitsPanel is the newest commits of the repository's history, drawn as a git graph. */
export const latestCommitsPanel: Panel<Params> = {
  id: "latest-commits",
  column: "side",
  prefetch: ({ api, qc, params: { owner, name } }) =>
    prefetch(qc, historyQuery(api, qc, owner, name, {}, latestSize, { page: 1 })),
  render: ({ owner, name }) => <LatestCommits owner={owner} name={name} />,
}
