// Typed fixtures: values copied from x-scp-fixtures of api/openapi-public.yaml. If the schema drifts, this file stops compiling.
import type { components } from "@/lib/api/schema"
type S = components["schemas"]

export const freshCatalog: S["Freshness"] = { status: "fresh", kind: "catalog.stats", asOf: "2026-10-14T09:40:51Z", fetchedAt: "2026-10-14T09:41:07Z", checkedAt: "2026-10-14T09:41:07Z", staleSince: null, lastErrorCode: null }
export const staleManifests: S["Freshness"] = { status: "stale", kind: "manifests.repo", asOf: "2026-10-14T07:40:11Z", fetchedAt: "2026-10-14T07:41:07Z", checkedAt: "2026-10-14T09:41:07Z", staleSince: "2026-10-14T08:26:07Z", lastErrorCode: "github.rate_limited" }

export const commitA: S["Commit"] = { sha: "3f2a9c1d7b5e4a0f8c6d2b1a9e7f5c3d1b0a8e6f", repo: "billing-service", subject: "Fix rounding of VAT on credit notes", authorLogin: "kari-n", authorName: "Kari Nordmann", committedAt: "2026-10-14T08:12:44Z", url: "https://github.com/acme/billing-service/commit/3f2a9c1d7b5e4a0f8c6d2b1a9e7f5c3d1b0a8e6f" }
export const commitB: S["Commit"] = { sha: "8d1e5b7a2c9f4036a1d8e7b5c2f09a4d6e3b1c87", repo: "core-lib", subject: "Bump guava to 33.3.1-jre", authorLogin: null, authorName: "Dependabot", committedAt: "2026-10-14T07:55:02Z", url: "https://github.com/acme/core-lib/commit/8d1e5b7a2c9f4036a1d8e7b5c2f09a4d6e3b1c87" }

const docBase: Pick<S["DocIndexEntry"], "description" | "format" | "bridged" | "state" | "tags" | "blobSha" | "updatedAt"> = { description: null, format: "markdown", bridged: false, state: "resident", tags: [], blobSha: "7a21c4e9d0b35f8a6e1d2c7b9f40a3e5d8c1b6f2", updatedAt: "2026-10-02T14:40:00Z" }
export const docReadme: S["DocIndexEntry"] = { ...docBase, id: "readme", title: "Billing Service", path: "README.md", order: null, section: [], uiUrl: "/repos/billing-service", apiUrl: "/api/v1/repos/billing-service/docs/readme", markdownUrl: "/repos/billing-service/readme.md", sourceUrl: "https://github.com/acme/billing-service/blob/3f2a9c1/README.md" }
export const docInstall: S["DocIndexEntry"] = { ...docBase, id: "docs~install.md", title: "Installation", description: "Installing and configuring the service", path: "docs/install.md", order: 10, tags: ["setup"], section: ["Guides"], uiUrl: "/repos/billing-service/docs/docs/install", apiUrl: "/api/v1/repos/billing-service/docs/docs~install.md", markdownUrl: "/repos/billing-service/docs/docs/install.md", sourceUrl: "https://github.com/acme/billing-service/blob/3f2a9c1/docs/install.md" }
export const docConfig: S["DocIndexEntry"] = { ...docInstall, id: "docs~config.md", title: "Configuration", path: "docs/config.md", order: 20, section: ["Guides", "Advanced"], uiUrl: "/repos/billing-service/docs/docs/config.md", apiUrl: "/api/v1/repos/billing-service/docs/docs~config.md", markdownUrl: "/repos/billing-service/docs/docs/config.md" }

export const groupOk: S["GroupSummary"] = { id: "platform", displayName: "Platform", description: "Shared libraries and infrastructure services.", repoCount: 18, defaultRepoState: "ok", defaultRepo: { slug: "core-lib", name: "core-lib", title: "Core Library", description: "Shared building blocks for Acme services.", htmlUrl: "https://github.com/acme/core-lib", uiUrl: "/repos/core-lib" }, integrations: ["jenkins"], badges: [{ kind: "issues", url: "/badges/core-lib/issues.svg", label: "open issues" }, { kind: "repos", url: "/badges/groups/platform/repos.svg", label: "repositories" }], uiUrl: "/groups/platform", commitsUiUrl: "/groups/platform/commits", apiUrl: "/api/v1/groups/platform", llmsUrl: "/groups/platform/llms.txt", freshness: freshCatalog }
export const groupMissing: S["GroupSummary"] = { id: "billing", displayName: "Billing", description: "Invoicing and payments.", repoCount: 7, defaultRepoState: "missing", integrations: [], badges: [{ kind: "repos", url: "/badges/groups/billing/repos.svg", label: "repositories" }], uiUrl: "/groups/billing", commitsUiUrl: "/groups/billing/commits", apiUrl: "/api/v1/groups/billing", llmsUrl: "/groups/billing/llms.txt", freshness: freshCatalog }

export const graphSmall: S["DependencyGraph"] = {
  nodes: [
    { id: "billing-service#.", repo: "billing-service", dir: ".", ecosystem: "maven", label: "billing-service", artifacts: ["pkg:maven/org.acme.billing/billing-service"], inDegree: 0, outDegree: 1, inCycle: false },
    { id: "core-lib#.", repo: "core-lib", dir: ".", ecosystem: "maven", label: "core-lib", artifacts: ["pkg:maven/org.acme.platform/core-lib"], inDegree: 1, outDegree: 1, inCycle: true },
    { id: "auth-client#.", repo: "auth-client", dir: ".", ecosystem: "npm", label: "auth-client", artifacts: [], inDegree: 1, outDegree: 1, inCycle: true },
  ],
  edges: [
    { from: "billing-service#.", to: "core-lib#.", component: "pkg:maven/org.acme.platform/core-lib", requirement: "${core.version}", version: "2.7.1", scope: "runtime", intra: false, versionStatus: "current" },
    { from: "core-lib#.", to: "auth-client#.", component: "pkg:npm/auth-client", requirement: "^1", version: "1.2.0", scope: "runtime", intra: false, versionStatus: "behind" },
    { from: "auth-client#.", to: "core-lib#.", component: "pkg:maven/org.acme.platform/core-lib", requirement: "2.5.0", version: "2.5.0", scope: "runtime", intra: false, versionStatus: "behind" },
  ],
  cycles: [["core-lib#.", "auth-client#."]],
  summary: { repos: 3, units: 3, edges: 3, cycles: 1, ecosystems: [{ ecosystem: "maven", units: 2, dependencies: 5 }, { ecosystem: "npm", units: 1, dependencies: 2 }] },
  truncated: false,
  freshness: staleManifests,
}
