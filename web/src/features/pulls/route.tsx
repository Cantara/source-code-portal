import { groupsQuery, pullsQuery } from "@/lib/api/queries"
import { crumbs } from "@/lib/crumbs"
import { list, prefetch } from "@/lib/load"
import type { Ctx, Route } from "@/lib/page"
import { pageSize, sizeCookie } from "@/lib/paging"
import { ld } from "@/lib/seo"
import { defaultPullSize, pullsState } from "./api"
import { PullsView } from "./view"

/** size is the reader's page size: the one they chose last, else the default. */
const size = (ctx: Ctx<unknown>) => pageSize(ctx.cookies[sizeCookie("pulls")], defaultPullSize)

/** pullsRoute is `/pulls`: every open pull request, whose turn first, filtered through the query string. */
export const pullsRoute: Route<object> = {
  load: (ctx) => {
    const st = pullsState(ctx.search, size(ctx))
    return Promise.all([
      list(ctx.qc, pullsQuery(ctx.api, { ...st.filter, page: st.page, size: st.size })),
      prefetch(ctx.qc, groupsQuery(ctx.api)),
    ])
  },
  metadata: () => ({
    title: "Pull requests",
    description: "The open pull requests across the repositories of the organisation, and whose turn each is.",
    alternates: { canonical: "/pulls" },
  }),
  structured: (_, base) => [ld.page(base, "/pulls", "Pull requests"), ld.breadcrumbs(base, crumbs.pulls())],
  render: (ctx) => <PullsView defaultSize={size(ctx)} />,
}
