import { groupsQuery, issuesQuery } from "@/lib/api/queries"
import { crumbs } from "@/lib/crumbs"
import { list, prefetch } from "@/lib/load"
import type { Ctx, Route } from "@/lib/page"
import { pageSize, sizeCookie } from "@/lib/paging"
import { ld } from "@/lib/seo"
import { defaultIssueSize, issuesState } from "./api"
import { IssuesView } from "./view"

/** size is the reader's page size: the one they chose last, else the default. */
const size = (ctx: Ctx<unknown>) => pageSize(ctx.cookies[sizeCookie("issues")], defaultIssueSize)

/** issuesRoute is `/issues`: every open issue, what waits first, filtered through the query string. */
export const issuesRoute: Route<object> = {
  load: (ctx) => {
    const st = issuesState(ctx.search, size(ctx))
    return Promise.all([
      list(ctx.qc, issuesQuery(ctx.api, { ...st.filter, page: st.page, size: st.size })),
      prefetch(ctx.qc, groupsQuery(ctx.api)),
    ])
  },
  metadata: () => ({
    title: "Issues",
    description: "The open issues across the repositories of the organisation, and where each stands.",
    alternates: { canonical: "/issues" },
  }),
  structured: (_, base) => [ld.page(base, "/issues", "Issues"), ld.breadcrumbs(base, crumbs.issues())],
  render: (ctx) => <IssuesView defaultSize={size(ctx)} />,
}
