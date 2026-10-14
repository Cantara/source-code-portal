import { queryOptions } from "@tanstack/react-query"
import { api, unwrap } from "@/lib/api/client"

export const dashboardQuery = queryOptions({
  queryKey: ["dashboard"],
  queryFn: ({ signal }) => unwrap(api.GET("/dashboard", { signal })),
})
