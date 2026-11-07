import { archiveQuery, groupsQuery } from "@/lib/api/queries"
import { crumbs } from "@/lib/crumbs"
import { list, prefetch } from "@/lib/load"
import type { Ctx, Route } from "@/lib/page"
import { pageSize, sizeCookie } from "@/lib/paging"
import { filtersCookie, filtersOf } from "@/lib/preferences"
import { ld } from "@/lib/seo"
import { archiveState, defaultArchiveSize } from "./api"
import { ArchiveView } from "./view"

/** size is the reader's page size: the one they chose last, else the default. */
const size = (ctx: Ctx<unknown>) => pageSize(ctx.cookies[sizeCookie("archive")], defaultArchiveSize)

/** archiveRoute is `/archive`: the retired repositories, filtered through the query string. */
export const archiveRoute: Route<object> = {
  load: (ctx) => {
    const st = archiveState(ctx.search, size(ctx))
    return Promise.all([
      list(ctx.qc, archiveQuery(ctx.api, { ...st.filter, page: st.page, size: st.size })),
      prefetch(ctx.qc, groupsQuery(ctx.api)),
    ])
  },
  metadata: () => ({
    title: "Archive",
    description:
      "The retired repositories of the organisation: archived or deprecated, when each was last active and what still depends on it.",
    alternates: { canonical: "/archive" },
  }),
  structured: (_, base) => [ld.page(base, "/archive", "Archive"), ld.breadcrumbs(base, crumbs.archive())],
  render: (ctx) => <ArchiveView defaultSize={size(ctx)} filters={filtersOf(ctx.cookies[filtersCookie])} />,
}
