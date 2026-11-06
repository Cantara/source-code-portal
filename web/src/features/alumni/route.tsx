import { alumniQuery, groupsQuery } from "@/lib/api/queries"
import { crumbs } from "@/lib/crumbs"
import { list, prefetch } from "@/lib/load"
import type { Ctx, Route } from "@/lib/page"
import { pageSize, sizeCookie } from "@/lib/paging"
import { ld } from "@/lib/seo"
import { alumniState, defaultAlumniSize } from "./api"
import { AlumniView } from "./view"

/** size is the reader's page size: the one they chose last, else the default. */
const size = (ctx: Ctx<unknown>) => pageSize(ctx.cookies[sizeCookie("alumni")], defaultAlumniSize)

/** alumniRoute is `/alumni`: the retired repositories, filtered through the query string. */
export const alumniRoute: Route<object> = {
  load: (ctx) => {
    const st = alumniState(ctx.search, size(ctx))
    return Promise.all([
      list(ctx.qc, alumniQuery(ctx.api, { ...st.filter, page: st.page, size: st.size })),
      prefetch(ctx.qc, groupsQuery(ctx.api)),
    ])
  },
  metadata: () => ({
    title: "Alumni",
    description:
      "The retired repositories of the organisation: archived or deprecated, when each was last active and what still depends on it.",
    alternates: { canonical: "/alumni" },
  }),
  structured: (_, base) => [ld.page(base, "/alumni", "Alumni"), ld.breadcrumbs(base, crumbs.alumni())],
  render: (ctx) => <AlumniView defaultSize={size(ctx)} />,
}
