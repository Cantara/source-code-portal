import { ProblemError } from "@/lib/api/client"
import type { Route } from "@/lib/page"
import { searchQuery, searchState } from "./api"
import { SearchView } from "./view"

/** searchRoute is `/search?q=`: the hits rendered on the server, so the page reads without JavaScript; never indexed. */
export const searchRoute: Route<Record<string, never>> = {
  load: async ({ api, qc, search }) => {
    const st = searchState(search)
    if (st.q) await qc.query(searchQuery(api, st)).catch(() => undefined)
  },
  metadata: ({ search }) => {
    const { q } = searchState(search)
    return { title: q ? `Search: ${q}` : "Search", robots: { index: false, follow: false } }
  },
  // A query the server refused renders its problem, so the page says why without JavaScript too.
  render: ({ api, qc, search }) => {
    const st = searchState(search)
    const e = st.q ? qc.getQueryState(searchQuery(api, st).queryKey)?.error : undefined
    return (
      <SearchView
        problem={
          e instanceof ProblemError ? { status: e.status, code: e.code, detail: e.message, requestId: e.requestId } : undefined
        }
      />
    )
  },
}
