import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"

export const dashboardQuery = (api: Api) =>
  queryOptions({ queryKey: keys.dashboard(), queryFn: ({ signal }) => unwrap(api.GET("/dashboard", { signal })) })
