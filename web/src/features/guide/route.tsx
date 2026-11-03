import type { Route } from "@/lib/page"
import { GuideView } from "./view"

/** guideRoute is `/guide`: getting started with the portal's signals and with running one (design/guide.md). No data, no JavaScript. */
export const guideRoute: Route<Record<string, never>> = {
  load: async () => {},
  metadata: () => ({
    title: "Getting started",
    description:
      "How to read the portal's signals (telemetry, the fleet map, product cards, health) and how to run your own portal.",
  }),
  render: () => <GuideView version={process.env.SCP_VERSION ?? "dev"} />,
}
