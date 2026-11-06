import { load, prefetch } from "@/lib/load"
import type { Route } from "@/lib/page"
import { dashboardQuery, pageQuery } from "./api"
import {
  columnCookie,
  columnsOf,
  commitsCookie,
  commitsOf,
  dismissedCookie,
  dismissedOf,
  productsView,
  profileCookie,
  profileNamed,
  presentSectionsCookie,
  sectionsCookie,
  sectionsOf,
  sideCookie,
  sideOf,
  viewCookie,
  workCookie,
  wallCookie,
  workOf,
} from "@/lib/preferences"
import { DashboardView } from "./view"
import { issuesQuery, pullsQuery } from "@/lib/api/queries"
import { issuesFilter, pullsFilter } from "./api"
import { PresentView } from "./present"
import type { Search } from "@/lib/page"
import { ld } from "@/lib/seo"

/** dashboardRoute is `/`: the dashboard and its layout, through the reader's lens on people's work and products view. */
export const dashboardRoute: Route<object> = {
  load: ({ api, qc, cookies }) => {
    const work = workOf(cookies[workCookie])
    return Promise.all([
      load(qc, dashboardQuery(api, work)),
      prefetch(qc, pageQuery(api)),
      prefetch(qc, pullsQuery(api, pullsFilter(work, 5))),
      prefetch(qc, issuesQuery(api, issuesFilter(work, 5))),
    ])
  },
  metadata: () => ({ title: { absolute: "Dashboard · Source Code Portal" }, alternates: { canonical: "/" } }),
  structured: (_, base) => [ld.page(base, "/", "Dashboard", "CollectionPage", false)],
  render: (ctx) => {
    const view = ctx.cookies[viewCookie]
    return (
      <DashboardView
        work={workOf(ctx.cookies[workCookie])}
        view={view ? productsView(view, "cards") : undefined}
        side={sideOf(ctx.cookies[sideCookie], ctx.cookies[sectionsCookie])}
        show={commitsOf(ctx.cookies[commitsCookie])}
        dismissed={dismissedOf(ctx.cookies[dismissedCookie])}
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
    Promise.all([
      load(qc, dashboardQuery(api, lens(search, cookies))),
      prefetch(qc, pageQuery(api, "present")),
      prefetch(qc, pullsQuery(api, pullsFilter(lens(search, cookies), 8))),
      prefetch(qc, issuesQuery(api, issuesFilter(lens(search, cookies), 8))),
    ]),
  metadata: () => ({
    title: "Presentation",
    robots: { index: false, follow: false },
    // Added to the home screen, it opens full screen at /present (app/manifest.ts).
    manifest: "/manifest.webmanifest",
    appleWebApp: { capable: true, title: "SCP", statusBarStyle: "black-translucent" },
  }),
  render: ({ cookies, search }) => (
    <PresentView
      work={lens(search, cookies)}
      only={only(search)}
      columns={columnsOf(cookies[columnCookie])}
      profile={profileNamed(cookies[profileCookie])}
      wall={cookies[wallCookie] === "1"}
      ask={typeof search.profile === "string" ? search.profile : undefined}
      show={commitsOf(cookies[commitsCookie])}
      hidden={sectionsOf(cookies[presentSectionsCookie])}
    />
  ),
}
