import { setupServer } from "msw/node"
import { createOpenApiHttp } from "openapi-msw"
import type { components, paths } from "@/lib/api/schema"

type S = components["schemas"]

/** http builds MSW handlers typed from the contract. */
export const http = createOpenApiHttp<paths>({ baseUrl: "*/api/v1" })

const at = "2026-10-13T10:00:00Z"
export const fresh: S["Freshness"] = { status: "fresh", fetchedAt: at, checkedAt: at }

export function commit(i: number, repo = "acme/core"): S["Commit"] {
  return {
    repo,
    sha: i.toString(16).padStart(40, "0"),
    subject: `Commit ${i}`,
    authorName: "Ada Lovelace",
    authorLogin: "ada",
    url: `https://github.com/${repo}/commit/${i}`,
    committedAt: at,
  }
}

export function repo(name: string, over: Partial<S["Repo"]> = {}): S["Repo"] {
  return {
    address: `acme/${name.toLowerCase()}`,
    owner: "acme",
    name,
    description: `The ${name} service`,
    htmlUrl: `https://github.com/acme/${name}`,
    homepage: "",
    defaultBranch: "main",
    license: "Apache-2.0",
    visibility: "public",
    topics: [],
    archived: false,
    fork: false,
    stars: 1,
    openIssues: 0,
    pushedAt: at,
    groups: ["iam"],
    freshness: fresh,
    ...over,
  }
}

export const descriptor: S["Descriptor"] = {
  present: true,
  file: "scp.yaml",
  title: "Core identity",
  description: "Identity server",
  lifecycle: "production",
  owners: ["@acme/iam"],
  tags: ["java"],
  links: [{ title: "Runbook", url: "https://runbook.example/core" }],
  problems: [],
  freshness: fresh,
}

export const snapshot = (version: number, ready = true): S["SnapshotInfo"] => ({ version, builtAt: at, ready, sources: [] })

/** defaults answer every page with one group, two repositories and a README. */
export const defaults = [
  http.get("/snapshot", ({ response }) => response(200).json(snapshot(1))),
  http.get("/dashboard", ({ response }) =>
    response(200).json({
      groups: [{ id: "iam", name: "IAM", description: "Identity and access", repoCount: 2, lastActivity: at }],
      repoCount: 3,
      ungroupedCount: 1,
      latest: [commit(1)],
      freshness: { fresh: 2, stale: 1, unknown: 0, error: 0 },
    }),
  ),
  http.get("/groups", ({ response }) =>
    response(200).json({
      groups: [{ id: "iam", name: "IAM", description: "", defaultRepo: null, defaultRepoState: "missing", repoCount: 2 }],
    }),
  ),
  http.get("/groups/{group}", ({ params, response }) =>
    params.group === "iam"
      ? response(200).json({
          id: "iam",
          name: "IAM",
          description: "Identity and access",
          defaultRepo: null,
          defaultRepoState: "missing",
          repoCount: 2,
          repos: [repo("Core", { pushedAt: "2026-10-01T00:00:00Z" }), repo("Agent", { description: "The agent" })],
        })
      : response(404).json({
          type: "about:blank",
          title: "Not Found",
          status: 404,
          code: "not-found",
          instance: "urn:scp:request:r1",
        }),
  ),
  http.get("/repos/{owner}/{name}", ({ params, response }) =>
    params.name.toLowerCase() === "core"
      ? response(200).json({
          repo: repo("Core"),
          title: "Core identity",
          descriptor,
          commits: [commit(1)],
          commitsFreshness: fresh,
        })
      : response(404).json({ type: "about:blank", title: "Not Found", status: 404, code: "not-found" }),
  ),
  http.get("/repos/{owner}/{name}/docs", ({ response }) =>
    response(200).json({
      docs: [
        {
          id: "README.md",
          path: "README.md",
          readme: true,
          title: "Core",
          description: "",
          format: "markdown",
          state: "ok",
          truncated: false,
        },
        {
          id: "docs/setup.md",
          path: "docs/setup.md",
          readme: false,
          title: "Setup",
          description: "",
          format: "markdown",
          state: "ok",
          truncated: false,
        },
      ],
      freshness: fresh,
    }),
  ),
  http.get("/repos/{owner}/{name}/docs/{docId}", ({ params, response }) =>
    response(200).json({
      id: params.docId,
      path: params.docId,
      readme: params.docId === "README.md",
      title: params.docId === "README.md" ? "Core" : "Setup",
      description: "",
      format: "markdown",
      state: "ok",
      truncated: false,
      html:
        params.docId === "README.md"
          ? "<h1>Core</h1><p>Hello from the README.</p>"
          : '<h2 id="install">Install</h2><p>Run it.</p>',
      toc: params.docId === "README.md" ? [] : [{ level: 2, id: "install", text: "Install" }],
      blobSha: "b1",
      sourceUrl: `https://github.com/acme/core/blob/HEAD/${params.docId}`,
      freshness: fresh,
    }),
  ),
  http.get("/commits", ({ response }) =>
    response(200).json({ commits: [commit(1)], page: { nextCursor: null, limit: 50 }, freshness: fresh }),
  ),
]

/** server answers the API in tests; an unmocked request fails the test. */
export const server = setupServer(...defaults)
