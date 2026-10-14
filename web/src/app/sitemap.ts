import type { MetadataRoute } from "next"
import { unwrap } from "@/lib/api/client"
import { origin, serverApi } from "@/lib/api/server"
import { groupPath, repoPath } from "@/lib/paths"

export const dynamic = "force-dynamic"

/** sitemap lists the dashboard, the commit log and every group and repository the viewer sees. */
export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const [api, base] = await Promise.all([serverApi(), origin()])
  const [groups, repos] = await Promise.all([unwrap(api.GET("/groups")), unwrap(api.GET("/repos"))])
  return [
    { url: `${base}/` },
    { url: `${base}/commits` },
    ...groups.groups.map((g) => ({ url: `${base}${groupPath(g.id)}` })),
    ...repos.repos.map((r) => ({ url: `${base}${repoPath(r.owner, r.name)}`, lastModified: r.pushedAt })),
  ]
}
