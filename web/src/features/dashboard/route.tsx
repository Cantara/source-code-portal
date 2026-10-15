import { reposQuery } from "@/lib/api/queries"
import { load, prefetch } from "@/lib/load"
import type { Route } from "@/lib/page"
import { dashboardQuery } from "./api"
import { DashboardView } from "./view"
import { ld } from "@/lib/seo"

/** dashboardRoute is `/`: the dashboard and every repository for the product cards. */
export const dashboardRoute: Route<object> = {
  load: ({ api, qc }) => Promise.all([load(qc, dashboardQuery(api)), prefetch(qc, reposQuery(api))]),
  metadata: () => ({ title: { absolute: "Dashboard · Source Code Portal" }, alternates: { canonical: "/" } }),
  structured: (_, base) => [ld.page(base, "/", "Dashboard", "CollectionPage", false)],
  render: () => <DashboardView />,
}
