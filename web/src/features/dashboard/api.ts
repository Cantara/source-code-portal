import { queryOptions } from "@tanstack/react-query"
import { get, unwrap, type Api } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"
import type { Element } from "./layout"

export const dashboardQuery = (api: Api) =>
  queryOptions({ queryKey: keys.dashboard(), queryFn: ({ signal }) => unwrap(api.GET("/dashboard", { signal })) })

/** pageQuery is the landing page's layout, resolved for the viewer. */
export const pageQuery = (api: Api) =>
  queryOptions({
    queryKey: keys.page("dashboard"),
    queryFn: ({ signal }) => unwrap(api.GET("/pages/{page}", { params: { path: { page: "dashboard" } }, signal })),
  })

/** latestAllQuery is the newest commits of every kind, for the dashboard's "include automation". */
export const latestAllQuery = (api: Api, limit: number) =>
  queryOptions({
    queryKey: keys.commitLog("", "", true, limit, 1),
    queryFn: ({ signal }) => unwrap(api.GET("/commits", { params: { query: { limit } }, signal })),
  })

/** StoredLayout is the admin's layout of a page as the design mode edits it (out of the contract, behind the admin role). */
export type StoredLayout = {
  page: string
  elements: Element[]
  revision: string
  stored: boolean
  writable: boolean
  problem: string
}

export const layoutPath = "/admin/layouts/dashboard"

export const storedLayoutQuery = () =>
  queryOptions({
    queryKey: ["admin", "layout", "dashboard"] as const,
    queryFn: ({ signal }) => get<StoredLayout>(layoutPath, signal),
    staleTime: 0,
  })
