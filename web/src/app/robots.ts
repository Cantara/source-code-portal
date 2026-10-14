import type { MetadataRoute } from "next"
import { origin } from "@/lib/api/server"

export const dynamic = "force-dynamic"

export default async function robots(): Promise<MetadataRoute.Robots> {
  const base = await origin()
  return { rules: { userAgent: "*", allow: "/", disallow: "/api/" }, sitemap: `${base}/sitemap.xml` }
}
