import type { Schemas } from "@/lib/api/client"

/** The package managers' names as people write them, by ecosystem. */
const ecosystems: Record<string, string> = {
  maven: "Maven",
  npm: "npm",
  pypi: "PyPI",
  cargo: "crates.io",
  go: "Go",
  swift: "Swift",
  cocoapods: "CocoaPods",
  nuget: "NuGet",
  rubygems: "RubyGems",
  packagist: "Packagist",
}

/** ecosystemLabel is a package manager's name for the pages; an unknown one shows as it came. */
export function ecosystemLabel(ecosystem: string): string {
  return ecosystems[ecosystem] ?? ecosystem
}

/** Shipped is a repository's latest release wherever it was published: GitHub, or the registry of its newest package. */
export interface Shipped {
  /** GitHub, or the package manager's name */
  from: string
  /** the tag, or the package's version */
  version: string
  url?: string
  at?: string
  /** the package's name; none for a GitHub release */
  name?: string
  ecosystem?: string
}

/**
 * shipped is the newer of a repository's latest GitHub release and its newest package (the rule /releases uses for the latest of a
 * repository); the GitHub release when the package has no date; none when it has neither.
 */
export function shipped(repo: Schemas["Repo"]): Shipped | undefined {
  const gh = repo.release
  const pkg = newestPackage(repo)
  if (gh && (!pkg?.publishedAt || gh.publishedAt >= pkg.publishedAt))
    return { from: "GitHub", version: gh.tag, url: gh.url, at: gh.publishedAt }
  if (!pkg) return undefined
  return {
    from: ecosystemLabel(pkg.ecosystem),
    version: pkg.version,
    url: pkg.url,
    at: pkg.publishedAt ?? undefined,
    name: pkg.name,
    ecosystem: pkg.ecosystem,
  }
}

/** newestPackage is the package of a repository with the newest release (the API lists the newest first); none without one. */
export function newestPackage(repo: Schemas["Repo"]): Schemas["Package"] | undefined {
  return repo.packages[0]
}
