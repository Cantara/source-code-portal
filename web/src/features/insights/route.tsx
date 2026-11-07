import { groupsQuery, insightsQuery } from "@/lib/api/queries"
import { crumbs } from "@/lib/crumbs"
import { load, prefetch } from "@/lib/load"
import type { Route } from "@/lib/page"
import { filtersCookie, filtersOf } from "@/lib/preferences"
import { ld } from "@/lib/seo"
import { InsightsView } from "./view"

/** groupOf is the product the address names: `?group=`, as the other lists. */
const groupOf = (search: Record<string, unknown>) => (typeof search.group === "string" ? search.group.slice(0, 100) : "")

/** insightsRoute is `/insights`: what the portal's held data says about the fleet, or about one product (design/insights.md). */
export const insightsRoute: Route<object> = {
  load: (ctx) => Promise.all([load(ctx.qc, insightsQuery(ctx.api, groupOf(ctx.search))), prefetch(ctx.qc, groupsQuery(ctx.api))]),
  metadata: () => ({
    title: "Insights",
    description:
      "How often the fleet ships, what it is built on, what it leans on, where knowledge sits with one person and what went quiet.",
    alternates: { canonical: "/insights" },
  }),
  structured: (_, base) => [ld.page(base, "/insights", "Insights"), ld.breadcrumbs(base, crumbs.insights())],
  render: (ctx) => <InsightsView filters={filtersOf(ctx.cookies[filtersCookie])} />,
}
