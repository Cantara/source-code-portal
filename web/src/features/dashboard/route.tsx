import { load } from "@/lib/load"
import type { Route } from "@/lib/page"
import { dashboardQuery } from "./api"
import { productsView, viewCookie } from "./layout"
import { DashboardView } from "./view"
import { ld } from "@/lib/seo"

/** dashboardRoute is `/`: the dashboard, with the reader's remembered products view. */
export const dashboardRoute: Route<object> = {
  load: ({ api, qc }) => load(qc, dashboardQuery(api)),
  metadata: () => ({ title: { absolute: "Dashboard · Source Code Portal" }, alternates: { canonical: "/" } }),
  structured: (_, base) => [ld.page(base, "/", "Dashboard", "CollectionPage", false)],
  render: (ctx) => {
    const view = ctx.cookies[viewCookie]
    return <DashboardView view={view ? productsView(view, "cards") : undefined} />
  },
}
