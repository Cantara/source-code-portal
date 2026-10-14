import { queryOptions } from "@tanstack/react-query"
import { api, unwrap } from "@/lib/api/client"
import { qk } from "@/lib/api/query-keys"

export const pageQuery = (slug: string) =>
  queryOptions({
    queryKey: qk.pages.get(slug),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/pages/{slug}", { params: { path: { slug } }, signal })),
    staleTime: 60_000,
  })
export const pagesQuery = () =>
  queryOptions({
    queryKey: qk.pages.list(),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/pages", { signal })),
    staleTime: 60_000,
  })
