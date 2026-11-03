import { load, prefetch } from "@/lib/load"
import type { Route } from "@/lib/page"
import { dashboardQuery, pageQuery } from "./api"
import { productsView, viewCookie, workCookie, workOf } from "@/lib/preferences"
import { DashboardView } from "./view"
import { ld } from "@/lib/seo"

/** dashboardRoute is `/`: the dashboard and its layout, through the reader's lens on people's work and products view. */
export const dashboardRoute: Route<object> = {
  load: ({ api, qc, cookies }) =>
    Promise.all([load(qc, dashboardQuery(api, workOf(cookies[workCookie]))), prefetch(qc, pageQuery(api))]),
  metadata: () => ({ title: { absolute: "Dashboard · Source Code Portal" }, alternates: { canonical: "/" } }),
  structured: (_, base) => [ld.page(base, "/", "Dashboard", "CollectionPage", false)],
  render: (ctx) => {
    const view = ctx.cookies[viewCookie]
    return <DashboardView work={workOf(ctx.cookies[workCookie])} view={view ? productsView(view, "cards") : undefined} />
  },
}
