import { assistantQuery } from "@/lib/api/queries"
import { load } from "@/lib/load"
import type { Route } from "@/lib/page"
import { AskView } from "./view"

/** askRoute is `/ask`: the assistant, for the viewers the operator allows. */
export const askRoute: Route<Record<string, never>> = {
  load: ({ api, qc }) => load(qc, assistantQuery(api)),
  metadata: () => ({ title: "Ask", robots: { index: false, follow: false } }),
  render: () => <AskView />,
}
