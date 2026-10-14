/** Query-key roots equal the SSE invalidation topics (api.SseInvalidate.topics), plus the composite `repo` root. */
export const qk = {
  snapshot: () => ["snapshot"] as const,
  site: () => ["site"] as const,
  catalog: {
    groups: (p?: object) => ["catalog", "groups", p ?? {}] as const,
    group: (groupId: string) => ["catalog", "group", groupId] as const,
  },
  commits: { list: (p: object) => ["commits", "list", p] as const },
  repo: (slug: string) => ["repo", slug] as const,
  docs: { list: (slug: string) => ["docs", slug, "list"] as const, get: (slug: string, id: string) => ["docs", slug, "doc", id] as const },
  manifests: { root: () => ["manifests"] as const },
  pages: { list: () => ["pages", "list"] as const, get: (slug: string) => ["pages", "page", slug] as const },
  status: () => ["status"] as const,
} as const

export type Topic = "site" | "catalog" | "commits" | "docs" | "manifests" | "pages" | "status"
/** Which query roots a topic invalidates. `repo` is a composite of all content topics. */
export const topicRoots: Record<Topic, readonly string[]> = {
  site: ["site"],
  catalog: ["catalog", "repo"],
  commits: ["commits", "repo"],
  docs: ["docs", "repo"],
  manifests: ["manifests", "repo"],
  pages: ["pages"],
  status: ["status"],
}
