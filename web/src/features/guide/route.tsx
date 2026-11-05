import { rulesQuery } from "@/lib/api/queries"
import { load } from "@/lib/load"
import type { Route } from "@/lib/page"
import { GuideView } from "./view"

/**
 * guideRoute is `/guide`: getting started with the portal's signals and with running one (design/guide.md). No JavaScript of its
 * own: the server's rules (the levels, the findings and what they cost, the ages of activity) are read here, so the page says
 * what the portal does today.
 */
export const guideRoute: Route<Record<string, never>> = {
  load: ({ api, qc }) => load(qc, rulesQuery(api)),
  metadata: () => ({
    title: "Getting started",
    description:
      "How to read the portal's signals (telemetry, the fleet map, product cards, health) and how to run your own portal.",
  }),
  render: ({ api, qc }) => (
    <GuideView version={process.env.SCP_VERSION ?? "dev"} rules={qc.getQueryData(rulesQuery(api).queryKey)} />
  ),
}
