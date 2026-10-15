import { meQuery } from "@/lib/api/queries"
import { serverGet } from "@/lib/api/server"
import { load, prefetch } from "@/lib/load"
import type { Route } from "@/lib/page"
import { adminStatusQuery } from "./api"
import { AdminView } from "./view"

/** adminRoute is `/admin`: the portal's status for an administrator. */
export const adminRoute: Route<Record<string, never>> = {
  load: async ({ api, qc }) => Promise.all([load(qc, meQuery(api)), prefetch(qc, adminStatusQuery(await serverGet()))]),
  metadata: () => ({ title: "Admin", robots: { index: false, follow: false } }),
  render: () => <AdminView />,
}
