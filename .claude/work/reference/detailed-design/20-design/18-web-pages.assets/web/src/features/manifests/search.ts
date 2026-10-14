import { z } from "zod"

const SCOPES = ["runtime", "dev", "test", "build", "optional", "peer", "provided", "unknown"] as const
const list = <T extends z.ZodType>(t: T, max: number) => z.array(t).max(max).optional().catch(undefined)
const ecosystem = z.string().regex(/^[a-z][a-z0-9-]*$/).max(32)
const slugOrUnit = z.string().min(1).max(420)
export const PURL = /^pkg:[a-z][a-z0-9.+-]*\/\S+$/

/** /repos/$repo/dependencies : maps 1:1 to listRepoDependencies (+ getRepoManifests for the header). */
export const repoDepsSearch = z.object({
  scope: list(z.enum(SCOPES), 8),
  relationship: z.enum(["direct", "transitive", "all"]).default("direct").catch("direct"),
  internal: z.boolean().optional().catch(undefined),
  ecosystem: list(ecosystem, 20),
  unit: z.string().min(1).max(401).optional().catch(undefined),
  q: z.string().min(1).max(200).optional().catch(undefined),
  sort: z.enum(["name", "-name", "ecosystem", "-ecosystem", "scope", "-scope"]).default("ecosystem").catch("ecosystem"),
})
export type RepoDepsSearch = z.infer<typeof repoDepsSearch>

export const DEPENDENCY_VIEWS = ["graph", "table", "versions", "licenses"] as const
/** /dependencies : one page, four views; every filter is a search param so a view is shareable. */
export const orgDepsSearch = z.object({
  view: z.enum(DEPENDENCY_VIEWS).default("graph").catch("graph"),
  group: z.string().regex(/^[a-z0-9-]{1,63}$/).optional().catch(undefined),
  focus: slugOrUnit.optional().catch(undefined),
  depth: z.union([z.literal(1), z.literal(2), z.literal(3)]).default(1).catch(1),
  ecosystem: list(ecosystem, 20),
  scope: list(z.enum(SCOPES), 8),
  intra: z.boolean().default(false).catch(false),
  // versions view
  q: z.string().min(1).max(200).optional().catch(undefined),
  origin: z.enum(["all", "internal", "external"]).default("all").catch("all"),
  minVersions: z.number().int().min(1).max(100).default(1).catch(1),
})
export type OrgDepsSearch = z.infer<typeof orgDepsSearch>

/** /packages?purl= : the purl is a query value, never a path segment. */
export const packageSearch = z.object({
  purl: z.string().max(1024).regex(PURL).optional().catch(undefined),
  scope: list(z.enum(SCOPES), 8),
  group: z.string().regex(/^[a-z0-9-]{1,63}$/).optional().catch(undefined),
})
export type PackageSearch = z.infer<typeof packageSearch>
