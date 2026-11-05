import { load, prefetch } from "@/lib/load"
import type { Route } from "@/lib/page"
import { dashboardQuery, pageQuery } from "./api"
import { columnCookie, columnOf, productsView, sideCookie, sideOf, viewCookie, workCookie, workOf } from "@/lib/preferences"
import { DashboardView } from "./view"
import { PresentView } from "./present"
import type { Search } from "@/lib/page"
import { ld } from "@/lib/seo"

/** dashboardRoute is `/`: the dashboard and its layout, through the reader's lens on people's work and products view. */
export const dashboardRoute: Route<object> = {
  load: ({ api, qc, cookies }) =>
    Promise.all([load(qc, dashboardQuery(api, workOf(cookies[workCookie]))), prefetch(qc, pageQuery(api))]),
  metadata: () => ({ title: { absolute: "Dashboard · Source Code Portal" }, alternates: { canonical: "/" } }),
  structured: (_, base) => [ld.page(base, "/", "Dashboard", "CollectionPage", false)],
  render: (ctx) => {
    const view = ctx.cookies[viewCookie]
    return (
      <DashboardView
        work={workOf(ctx.cookies[workCookie])}
        view={view ? productsView(view, "cards") : undefined}
        side={sideOf(ctx.cookies[sideCookie])}
      />
    )
  },
}

/** only is the groups the address names (`?groups=core,tools`), or none: every group. */
function only(search: Search): string[] | undefined {
  const v = search.groups
  const ids = (Array.isArray(v) ? v.join(",") : (v ?? "")).split(",").filter(Boolean)
  return ids.length > 0 ? ids : undefined
}

/** lens is the work that counts: the address's (`?work=hand,ai`), else the reader's. */
const lens = (search: Search, cookies: Record<string, string>) => workOf(search.work ?? cookies[workCookie])

/**
 * presentRoute is `/present`, the presentation page for a tablet or a screen on a wall (design/present.md): the dashboard's data
 * through its own layout, on one screen. The address chooses the products and the work, so each screen is a bookmark; never indexed.
 */
export const presentRoute: Route<object> = {
  load: ({ api, qc, cookies, search }) =>
    Promise.all([load(qc, dashboardQuery(api, lens(search, cookies))), prefetch(qc, pageQuery(api, "present"))]),
  metadata: () => ({
    title: "Presentation",
    robots: { index: false, follow: false },
    // Added to the home screen, it opens full screen at /present (app/manifest.ts).
    manifest: "/manifest.webmanifest",
    appleWebApp: { capable: true, title: "SCP", statusBarStyle: "black-translucent" },
  }),
  render: ({ cookies, search }) => (
    <PresentView work={lens(search, cookies)} only={only(search)} column={columnOf(cookies[columnCookie])} />
  ),
}
