import { groupsQuery, releasesQuery } from "@/lib/api/queries"
import { crumbs } from "@/lib/crumbs"
import { list, prefetch } from "@/lib/load"
import type { Ctx, Route } from "@/lib/page"
import { pageSize, sizeCookie } from "@/lib/paging"
import { ld } from "@/lib/seo"
import { defaultReleaseSize, releasesState } from "./api"
import { ReleasesView } from "./view"

/** size is the reader's page size: the one they chose last, else the default. */
const size = (ctx: Ctx<unknown>) => pageSize(ctx.cookies[sizeCookie("releases")], defaultReleaseSize)

/** releasesRoute is `/releases`: the recent releases of the organisation, filtered through the query string. */
export const releasesRoute: Route<object> = {
  load: (ctx) => {
    const st = releasesState(ctx.search, size(ctx))
    return Promise.all([
      list(ctx.qc, releasesQuery(ctx.api, { ...st.filter, page: st.page, size: st.size })),
      prefetch(ctx.qc, groupsQuery(ctx.api)),
    ])
  },
  metadata: () => ({
    title: "Releases",
    description: "The recent releases of the repositories of the organisation, the latest of each or every one, newest first.",
    alternates: { canonical: "/releases" },
  }),
  structured: (_, base) => [ld.page(base, "/releases", "Releases"), ld.breadcrumbs(base, crumbs.releases())],
  render: (ctx) => <ReleasesView defaultSize={size(ctx)} />,
}
