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
    parents: i > 0 ? [(i - 1).toString(16).padStart(40, "0")] : [],
  }
}

export const healthy: S["Health"] = { score: 100, level: "good", findings: [] }

/** product is a product's health with one repository at attention. */
export const product: S["ProductHealth"] = {
  score: 74,
  level: "watch",
  counted: 2,
  attention: 1,
  watch: 0,
  findings: [{ repo: "acme/agent", code: "dormant", penalty: 30, message: "No change for 3 years: archive it or revive it" }],
  findingCount: 1,
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
    lastCommitAt: at,
    lastAuthoredAt: at,
    release: { tag: "v1.0.0", url: `https://github.com/acme/${name}/releases/tag/v1.0.0`, publishedAt: at },
    language: "Go",
    groups: ["iam"],
    health: healthy,
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

/** me is the viewer: anonymous on a portal with both ways to sign in, unless over says otherwise. */
export const me = (over: Partial<S["Me"]> = {}): S["Me"] => ({
  signedIn: false,
  name: "",
  via: "none",
  private: false,
  admin: false,
  signIn: { oidc: true, token: true },
  ...over,
})

/** defaults answer every page with one group, two repositories and a README. */
export const defaults = [
  http.get("/me", ({ response }) => response(200).json(me())),
  http.get("/assistant", ({ response }) =>
    response(200).json({ enabled: false, allowed: false, access: "signed-in", provider: "", model: "" }),
  ),
  http.get("/snapshot", ({ response }) => response(200).json(snapshot(1))),
  http.get("/repos", ({ response }) => response(200).json({ repos: [repo("Whydah"), repo("Whydah-Core")] })),
  http.get("/dashboard", ({ response }) =>
    response(200).json({
      groups: [
        {
          id: "iam",
          name: "IAM",
          description: "Identity and access",
          defaultRepo: "acme/whydah",
          repoCount: 2,
          lastActivity: at,
          health: product,
        },
      ],
      health: product,
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
          health: product,
          repos: [
            repo("Core", { pushedAt: "2026-10-01T00:00:00Z" }),
            repo("Agent", {
              description: "The agent",
              health: {
                score: 40,
                level: "attention",
                findings: [{ code: "dormant", penalty: 30, message: "No change for 3 years" }],
              },
            }),
          ],
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
          group: null,
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
  http.get("/repos/{owner}/{name}/manifests", ({ response }) =>
    response(200).json({
      technology: {
        languages: ["Java"],
        buildTools: ["Maven"],
        toolchains: [{ name: "java", versions: ["21"] }],
      },
      manifests: [
        {
          path: "pom.xml",
          ecosystem: "maven",
          name: "no.acme:core",
          version: "1.0",
          language: "Java",
          buildTool: "Maven",
          toolchain: [{ name: "java", version: "21" }],
          dependencies: [
            { name: "no.acme:lib", version: "2.0", scope: "runtime", repos: ["acme/lib"] },
            { name: "org.junit.jupiter:junit-jupiter", version: "5.12", scope: "test", repos: [] },
          ],
          problems: [],
        },
      ],
      uses: [{ address: "acme/lib", owner: "acme", name: "lib", via: ["no.acme:lib"] }],
      usedBy: [{ address: "acme/sso", owner: "acme", name: "sso", via: ["no.acme:core"] }],
      freshness: fresh,
    }),
  ),
  http.get("/commits", ({ response }) =>
    response(200).json({ commits: [commit(1)], page: { number: 1, size: 50, total: 1, pages: 1 }, freshness: fresh }),
  ),
  http.get("/repos/{owner}/{name}/history", ({ params, response }) =>
    response(200).json({
      commits: [1, 0].map((i) => ({
        ...commit(i, `${params.owner}/${params.name}`),
        body: "",
        committerName: "",
        automated: false,
        labels: [],
        beyond: [],
        graph: {
          lane: 0,
          color: 0,
          merge: false,
          above: i === 1 ? [] : [{ from: 0, to: 0, color: 0, open: false }],
          below: i === 1 ? [{ from: 0, to: 0, color: 0, open: false }] : [],
        },
      })),
      labels: [],
      filtered: false,
      lanes: 1,
      at: null,
      page: { number: 1, size: 30, total: 2, pages: 1 },
      freshness: fresh,
    }),
  ),
]

/** server answers the API in tests; an unmocked request fails the test. */
export const server = setupServer(...defaults)
