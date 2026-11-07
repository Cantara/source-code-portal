import { setupServer } from "msw/node"
import { createOpenApiHttp } from "openapi-msw"
import type { components, paths } from "@/lib/api/schema"
import { workOf } from "@/lib/preferences"
import serverRules from "./rules.json"

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
    kind: "person",
    assistant: "",
  }
}

/** pullCounts are open pull requests by state; noPulls none. */
export function pullCounts(over: Partial<S["PullCounts"]["states"]> = {}, oldestReview: string | null = null): S["PullCounts"] {
  const states = { draft: 0, conflicts: 0, failing: 0, changes: 0, review: 0, checks: 0, ready: 0, unknown: 0, ...over }
  const open = Object.values(states).reduce((a, b) => a + b, 0)
  return { open, people: open, automation: 0, states, oldestReview }
}

export const noPulls = pullCounts()

/** pull is an open pull request of acme/core. */
export function pull(n: number, over: Partial<S["PullRequest"]> = {}): S["PullRequest"] {
  return {
    repo: "acme/core",
    number: n,
    title: `Change ${n}`,
    url: `https://github.com/acme/core/pull/${n}`,
    author: "ada",
    bot: false,
    draft: false,
    fork: false,
    createdAt: at,
    updatedAt: at,
    base: "main",
    head: `change-${n}`,
    labels: [],
    requested: 0,
    state: "review",
    waitsFor: "reviewers",
    checks: "passing",
    stats: { additions: 12, deletions: 3, files: 2 },
    ...over,
  }
}

/** issueCounts are open issues by state, all of kind other. */
export function issueCounts(
  over: Partial<S["IssueCounts"]["states"]> = {},
  oldestTriage: string | null = null,
): S["IssueCounts"] {
  const states = { triage: 0, unassigned: 0, assigned: 0, ...over }
  const open = Object.values(states).reduce((a, b) => a + b, 0)
  return { open, people: open, automation: 0, states, kinds: { bug: 0, feature: 0, question: 0, other: open }, oldestTriage }
}

/** issue is an open issue of acme/core. */
export function issue(n: number, over: Partial<S["Issue"]> = {}): S["Issue"] {
  return {
    repo: "acme/core",
    number: n,
    title: `Problem ${n}`,
    url: `https://github.com/acme/core/issues/${n}`,
    author: "ada",
    bot: false,
    createdAt: at,
    updatedAt: at,
    labels: [],
    assignees: [],
    assigned: 0,
    comments: 0,
    state: "triage",
    kind: "other",
    ...over,
  }
}

/** rules are the server's rules (GET /rules), held equal to internal/health by cmd/scp's TestRulesFixture. */
export const rules = serverRules as S["Rules"]

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
    openIssuesWithPulls: false,
    openPulls: 0,
    pushedAt: at,
    lastCommitAt: at,
    lastAuthoredAt: at,
    release: { tag: "v1.0.0", url: `https://github.com/acme/${name}/releases/tag/v1.0.0`, publishedAt: at },
    packages: [],
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

/** activity is a product's or the fleet's work, none unless over says otherwise. */
export const activity = (over: Partial<S["Activity"]> = {}): S["Activity"] => ({
  hand: 0,
  ai: 0,
  automation: 0,
  sweeps: 0,
  weeklyHand: [0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 3],
  weeklyAi: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
  weeklyAutomation: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
  assistants: [],
  authors: [],
  authorCount: 0,
  lastAuthoredAt: null,
  recent: [],
  ...over,
})

/** defaults answer every page with one group, two repositories and a README. */
export const defaults = [
  http.get("/me", ({ response }) => response(200).json(me())),
  http.get("/assistant", ({ response }) =>
    response(200).json({ enabled: false, allowed: false, access: "signed-in", provider: "", model: "" }),
  ),
  http.get("/rules", ({ response }) => response(200).json(rules)),
  http.get("/snapshot", ({ response }) => response(200).json(snapshot(1))),
  http.get("/pages/{page}", ({ response }) =>
    response(200).json({
      page: "dashboard",
      stored: false,
      announcements: [],
      elements: [
        { kind: "telemetry" },
        { kind: "fleet-map" },
        { kind: "products", view: "cards" },
        { kind: "latest-commits", count: 10 },
      ],
    }),
  ),
  http.get("/repos", ({ response }) => response(200).json({ repos: [repo("Whydah"), repo("Whydah-Core")] })),
  http.get("/dashboard", ({ query, response }) =>
    response(200).json({
      work: workOf(query.get("work")),
      groups: [
        {
          id: "iam",
          name: "IAM",
          description: "Identity and access",
          defaultRepo: "acme/whydah",
          repoCount: 2,
          lastActivity: at,
          health: product,
          activity: activity({
            hand: 1,
            ai: 3,
            weeklyAi: [0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 2],
            assistants: ["Claude"],
            automation: 9,
            weeklyAutomation: [0, 0, 0, 0, 0, 0, 0, 0, 0, 4, 0, 5],
            authors: [{ name: "Ada Lovelace", login: "ada" }, { name: "Bo" }],
            authorCount: 2,
            lastAuthoredAt: at,
            recent: [{ repo: "acme/whydah-core", at }],
          }),
          members: [
            { address: "acme/whydah", name: "Whydah", archived: false, lastAuthoredAt: at, level: "good", activity: "active" },
            {
              address: "acme/whydah-core",
              name: "Whydah-Core",
              archived: false,
              lastAuthoredAt: at,
              level: "watch",
              activity: "active",
            },
          ],
          summary: { license: "Apache-2.0", language: "Java", released: 1 },
          pulls: pullCounts({ review: 2, ready: 1 }, at),
        },
      ],
      health: product,
      activity: activity({
        hand: 1,
        ai: 3,
        assistants: ["Claude", "Copilot"],
        automation: 9,
        authors: [{ name: "Ada Lovelace", login: "ada" }, { name: "Bo" }],
        authorCount: 2,
        lastAuthoredAt: at,
      }),
      repoCount: 3,
      ungroupedCount: 1,
      latest: [{ ...commit(1), kind: "assisted", assistant: "Claude Opus 5" }],
      freshness: { fresh: 2, stale: 1, unknown: 0, error: 0 },
      pulls: pullCounts({ review: 2, ready: 1 }, at),
    }),
  ),
  http.get("/pulls", ({ response }) =>
    response(200).json({
      pulls: [pull(1), pull(2, { state: "ready", waitsFor: "maintainer" })],
      counts: pullCounts({ review: 1, ready: 1 }, at),
      page: { number: 1, size: 50, total: 2, pages: 1 },
      freshness: fresh,
    }),
  ),
  http.get("/prerequisites", ({ response }) => response(200).json({ unmet: [] })),
  http.get("/issues", ({ response }) =>
    response(200).json({
      issues: [
        issue(1),
        issue(2, { state: "assigned", assignees: ["bo"], assigned: 1, kind: "bug", labels: [{ name: "bug", color: "d73a4a" }] }),
      ],
      counts: issueCounts({ triage: 1, assigned: 1 }, at),
      page: { number: 1, size: 50, total: 2, pages: 1 },
      freshness: fresh,
    }),
  ),
  http.get("/releases", ({ response }) =>
    response(200).json({
      releases: [
        {
          repo: "acme/core",
          name: "core",
          groups: ["iam"],
          source: "github",
          package: "",
          tag: "v2.1.0-rc1",
          title: "Release candidate",
          summary: "Faster login",
          url: "https://github.com/acme/core/releases/tag/v2.1.0-rc1",
          publishedAt: "2026-11-05T10:00:00Z",
          prerelease: true,
          author: "ann",
          bot: false,
          assets: 2,
          notes: "<p><strong>Highlights</strong></p><ul><li>Faster login</li></ul>",
          notesCut: true,
          notesFrom: "release",
        },
        {
          repo: "acme/web",
          name: "web",
          groups: [],
          source: "github",
          package: "",
          tag: "v0.3.0",
          title: "",
          summary: "",
          url: "https://github.com/acme/web/releases/tag/v0.3.0",
          publishedAt: "2026-11-01T10:00:00Z",
          prerelease: false,
          author: "renovate[bot]",
          bot: true,
          assets: 0,
          notes: "<ul><li>a fix</li></ul>",
          notesCut: false,
          notesFrom: "changelog",
        },
      ],
      counts: { latest: 2, all: 3, stable: 2, pre: 1, github: 2, packages: 1 },
      page: { number: 1, size: 50, total: 2, pages: 1 },
      freshness: fresh,
    }),
  ),
  http.get("/insights", ({ response }) =>
    response(200).json({
      repos: 5,
      thresholds: { months: 12, releases: 5, singleAuthorShare: 90, singleAuthorCommits: 20, sharedLibraries: 10 },
      cadence: {
        months: Array.from({ length: 12 }, (_, i) => ({
          month: `2026-${String(i + 1).padStart(2, "0")}`,
          releases: i === 10 ? 3 : 0,
        })),
        products: [
          {
            group: "iam",
            name: "Identity",
            releases: 3,
            medianDays: 12,
            last: "2026-11-05T10:00:00Z",
            weekly: Array.from({ length: 52 }, (_, i) => (i === 51 ? 2 : i === 47 ? 1 : 0)),
          },
          {
            group: "tools",
            name: "Tools",
            releases: 0,
            medianDays: null,
            last: null,
            weekly: Array.from({ length: 52 }, () => 0),
          },
        ],
      },
      technology: {
        languages: [{ name: "Go", repos: 3, share: 0.6 }],
        buildTools: [{ name: "Go modules", repos: 3, share: 0.6 }],
        runtimes: [
          {
            name: "go",
            repos: 3,
            oldest: "1.21",
            newest: "1.27",
            versions: [
              { version: "1.21", repos: 1 },
              { version: "1.27", repos: 2 },
            ],
          },
        ],
        unknown: 1,
      },
      libraries: [
        { repo: "acme/core", dependants: 12, dependantRepos: ["acme/api", "acme/web"], release: "v2.0.0" },
        { repo: "acme/old-lib", dependants: 1, dependantRepos: ["acme/web"], release: null, retired: "archived" },
      ],
      matrix: {
        libraries: ["acme/core", "acme/old-lib"],
        repos: [
          { repo: "acme/web", uses: [0, 1] },
          { repo: "acme/api", uses: [0] },
        ],
      },
      concentration: Array.from({ length: 10 }, (_, i) => ({
        from: i * 10,
        to: (i + 1) * 10,
        repos: i === 9 ? 1 : i === 4 ? 3 : 0,
      })),
      singleAuthor: [{ repo: "acme/api", commits: 48 }],
      ageing: {
        products: [{ group: "iam", name: "Identity", active: 2, recent: 1, quiet: 1, dormant: 1 }],
        active: 2,
        recent: 1,
        quiet: 1,
        dormant: 1,
        dormantRepos: [{ repo: "acme/quiet", lastChange: "2024-09-01T00:00:00Z" }],
      },
      freshness: fresh,
    }),
  ),
  http.get("/archive", ({ response }) =>
    response(200).json({
      archive: [
        {
          repo: repo("old-lib", { archived: true, description: "Retired in 2025" }),
          reason: "archived",
          lastActive: "2025-01-10T00:00:00Z",
          usedBy: 2,
          authors: 3,
        },
        { repo: repo("legacy-app"), reason: "deprecated", lastActive: "2026-03-01T00:00:00Z", usedBy: 0, authors: 1 },
      ],
      counts: { total: 2, archived: 1, deprecated: 1, used: 1 },
      page: { number: 1, size: 50, total: 2, pages: 1 },
      freshness: fresh,
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
