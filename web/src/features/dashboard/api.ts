import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"

export const dashboardQuery = (api: Api) =>
  queryOptions({
    queryKey: ["dashboard"],
    queryFn: ({ signal }) => unwrap(api.GET("/dashboard", { signal })),
  })
