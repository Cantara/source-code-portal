import { queryOptions } from "@tanstack/react-query"
import { get, unwrap, type Api } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"
import type { Work } from "@/lib/preferences"
import type { Element } from "./layout"

/** dashboardQuery is the dashboard through the reader's lens, the work that counts. */
export const dashboardQuery = (api: Api, work: Work) =>
  queryOptions({
    queryKey: keys.dashboard(work),
    queryFn: ({ signal }) => unwrap(api.GET("/dashboard", { params: { query: { work } }, signal })),
  })

/** Page is a page that has a layout: the landing page or the presentation page. */
export type Page = "dashboard" | "present"

/** pageQuery is a page's layout, resolved for the viewer. */
export const pageQuery = (api: Api, page: Page = "dashboard") =>
  queryOptions({
    queryKey: keys.page(page),
    queryFn: ({ signal }) => unwrap(api.GET("/pages/{page}", { params: { path: { page } }, signal })),
  })

/** workQuery is the newest commits of the work that counts, as many as limit, for the presentation page's column. */
export const workQuery = (api: Api, work: Work, limit: number) =>
  queryOptions({
    queryKey: [...keys.commitLog("", "", true, limit, 1), work.join(",")],
    queryFn: ({ signal }) => unwrap(api.GET("/commits", { params: { query: { work, limit } }, signal })),
  })

/** latestAllQuery is the newest commits of every kind, automation and sweeps included, for the dashboard's "include automation". */
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

/** layoutPath is the admin's address of a page's stored layout. */
export const layoutPath = (page: Page) => `/admin/layouts/${page}`

export const storedLayoutQuery = (page: Page) =>
  queryOptions({
    queryKey: ["admin", "layout", page] as const,
    queryFn: ({ signal }) => get<StoredLayout>(layoutPath(page), signal),
    staleTime: 0,
  })
