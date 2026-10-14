import { z } from "zod"
import type { components } from "@/lib/api/schema"

export const REPO_SORTS = ["name", "-name", "pushedAt", "-pushedAt", "lastCommit", "-lastCommit", "openIssues", "-openIssues", "lifecycle", "-lifecycle"] as const
export type RepoSort = (typeof REPO_SORTS)[number]
type Lifecycle = components["schemas"]["Lifecycle"]
type RepoType = components["schemas"]["RepoType"]

const arr = <T extends z.ZodType>(t: T) => z.array(t).max(10).optional().catch(undefined)

/** /groups/$groupId : every field maps 1:1 to a listGroupRepos query parameter (server-side filtering and sorting). */
export const groupSearch = z.object({
  q: z.string().min(1).max(200).optional().catch(undefined),
  lifecycle: arr(z.enum(["experimental", "production", "deprecated"] satisfies Lifecycle[])),
  type: arr(z.enum(["service", "library", "website", "tool", "documentation", "other"] satisfies RepoType[])),
  tag: arr(z.string().min(1).max(63)),
  archived: z.enum(["include", "exclude", "only"]).default("include").catch("include"),
  sort: z.enum(REPO_SORTS).default("name").catch("name"),
})
export type GroupSearch = z.infer<typeof groupSearch>
