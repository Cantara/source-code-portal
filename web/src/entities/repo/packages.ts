import type { Schemas } from "@/lib/api/client"

/** The package managers' names as people write them, by ecosystem. */
const ecosystems: Record<string, string> = { maven: "Maven", npm: "npm" }

/** ecosystemLabel is a package manager's name for the pages; an unknown one shows as it came. */
export function ecosystemLabel(ecosystem: string): string {
  return ecosystems[ecosystem] ?? ecosystem
}

/** newestPackage is the package of a repository with the newest release (the API lists the newest first); none without one. */
export function newestPackage(repo: Schemas["Repo"]): Schemas["Package"] | undefined {
  return repo.packages[0]
}
