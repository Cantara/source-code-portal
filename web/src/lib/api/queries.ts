import { keepPreviousData, queryOptions } from "@tanstack/react-query"
import { unwrap, type Api, type Schemas } from "./client"

const lc = (s: string) => s.toLowerCase()

/**
 * keys are the query keys of the resources several features read; a feature's own queries build theirs here too, so the server's
 * cache and the browser's always agree on a key.
 */
export const keys = {
  rules: () => ["rules"] as const,
  dashboard: (work: readonly string[]) => ["dashboard", work.join(",")] as const,
  repos: () => ["repos"] as const,
  repo: (owner: string, name: string) => ["repo", lc(owner), lc(name)] as const,
  docs: (owner: string, name: string) => ["docs", lc(owner), lc(name)] as const,
  doc: (owner: string, name: string, id: string) => ["doc", lc(owner), lc(name), id] as const,
  manifests: (owner: string, name: string) => ["manifests", lc(owner), lc(name)] as const,
  groups: () => ["groups"] as const,
  group: (group: string) => ["group", group] as const,
  commitLog: (group = "", repo = "", automation = true, size = 50, page = 1) =>
    ["commits", "log", group, repo, automation, size, page] as const,
  history: (owner: string, name: string, filter: Record<string, string | number | boolean | undefined>) =>
    ["commits", "history", lc(owner), lc(name), filter] as const,
  latestCommits: (group: string) => ["commits", "latest", group] as const,
  pulls: (f: PullFilter) =>
    [
      "pulls",
      f.group ?? "",
      f.repo ?? "",
      f.state ?? "",
      f.by ?? "",
      f.drafts ?? false,
      f.sort ?? "waiting",
      f.size ?? 50,
      f.page ?? 1,
    ] as const,
  releases: (f: ReleaseFilter) =>
    [
      "releases",
      f.group ?? "",
      f.repo ?? "",
      f.variant ?? "latest",
      f.since ?? "",
      f.sort ?? "newest",
      f.size ?? 50,
      f.page ?? 1,
    ] as const,
  insights: (group: string) => ["insights", group] as const,
  archive: (f: ArchiveFilter) =>
    [
      "archive",
      f.group ?? "",
      f.q ?? "",
      f.reason ?? "",
      f.used ?? false,
      f.sort ?? "active",
      f.size ?? 50,
      f.page ?? 1,
    ] as const,
  issues: (f: IssueFilter) =>
    [
      "issues",
      f.group ?? "",
      f.repo ?? "",
      f.state ?? "",
      f.kind ?? "",
      f.by ?? "",
      f.sort ?? "waiting",
      f.size ?? 50,
      f.page ?? 1,
    ] as const,
  page: (page: string) => ["page", page] as const,
  me: () => ["me"] as const,
  prerequisites: () => ["prerequisites"] as const,
  assistant: () => ["assistant"] as const,
  search: (q: string, kind: string, page: number) => ["search", q, kind, page] as const,
  suggest: (q: string) => ["search", "suggest", q] as const,
}

export const meQuery = (api: Api) =>
  queryOptions({ queryKey: keys.me(), queryFn: ({ signal }) => unwrap(api.GET("/me", { signal })) })

/** prerequisitesQuery is the features whose GitHub prerequisites do not hold (design/prerequisites.md); an administrator gets the fix. */
export const prerequisitesQuery = (api: Api) =>
  queryOptions({ queryKey: keys.prerequisites(), queryFn: ({ signal }) => unwrap(api.GET("/prerequisites", { signal })) })

export const rulesQuery = (api: Api) =>
  queryOptions({ queryKey: keys.rules(), queryFn: ({ signal }) => unwrap(api.GET("/rules", { signal })) })

export const assistantQuery = (api: Api) =>
  queryOptions({ queryKey: keys.assistant(), queryFn: ({ signal }) => unwrap(api.GET("/assistant", { signal })) })

export const reposQuery = (api: Api) =>
  queryOptions({ queryKey: keys.repos(), queryFn: ({ signal }) => unwrap(api.GET("/repos", { signal })) })

export const repoQuery = (api: Api, owner: string, name: string) =>
  queryOptions({
    queryKey: keys.repo(owner, name),
    queryFn: ({ signal }) => unwrap(api.GET("/repos/{owner}/{name}", { params: { path: { owner, name } }, signal })),
  })

export const docListQuery = (api: Api, owner: string, name: string) =>
  queryOptions({
    queryKey: keys.docs(owner, name),
    queryFn: ({ signal }) => unwrap(api.GET("/repos/{owner}/{name}/docs", { params: { path: { owner, name } }, signal })),
  })

export const docQuery = (api: Api, owner: string, name: string, docId: string) =>
  queryOptions({
    queryKey: keys.doc(owner, name, docId),
    queryFn: ({ signal }) =>
      unwrap(api.GET("/repos/{owner}/{name}/docs/{docId}", { params: { path: { owner, name, docId } }, signal })),
  })

export const groupsQuery = (api: Api) =>
  queryOptions({ queryKey: keys.groups(), queryFn: ({ signal }) => unwrap(api.GET("/groups", { signal })) })

export const groupQuery = (api: Api, group: string) =>
  queryOptions({
    queryKey: keys.group(group),
    queryFn: ({ signal }) => unwrap(api.GET("/groups/{group}", { params: { path: { group } }, signal })),
  })

/** PullFilter is a list of open pull requests (GET /pulls): its scope, state, whose and order, and the page. */
export interface PullFilter {
  group?: string
  repo?: string
  state?: Schemas["PullState"]
  by?: "people" | "automation"
  drafts?: boolean
  sort?: Schemas["QueueSort"]
  page?: number
  size?: number
}

/** insightsQuery is what the held data says about the fleet, or about one product (GET /insights). */
export const insightsQuery = (api: Api, group = "") =>
  queryOptions({
    queryKey: keys.insights(group),
    queryFn: ({ signal }) => unwrap(api.GET("/insights", { params: { query: { group: group || undefined } }, signal })),
    placeholderData: keepPreviousData,
  })

/** ReleaseFilter is a list of releases (GET /releases): its scope, variant, window and order, and the page. */
export interface ReleaseFilter {
  group?: string
  repo?: string
  variant?: Schemas["ReleaseVariant"]
  source?: Schemas["ReleaseSource"]
  since?: string
  sort?: Schemas["ReleaseSort"]
  page?: number
  size?: number
}

/** releasesQuery is one page of releases, newest first; /releases and the repository's and group's panels read it. */
export const releasesQuery = (api: Api, f: ReleaseFilter) =>
  queryOptions({
    queryKey: keys.releases(f),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/releases", {
          params: {
            query: {
              group: f.group,
              repo: f.repo,
              variant: f.variant,
              source: f.source,
              since: f.since,
              sort: f.sort,
              page: f.page,
              limit: f.size,
            },
          },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
  })

/** ArchiveFilter is a list of retired repositories (GET /archive): its group, name text, reason, whether still used, the order and the page. */
export interface ArchiveFilter {
  group?: string
  q?: string
  reason?: Schemas["ArchiveReason"]
  used?: boolean
  sort?: Schemas["ArchiveSort"]
  page?: number
  size?: number
}

/** archiveQuery is one page of the retired repositories, last active first; /archive reads it. */
export const archiveQuery = (api: Api, f: ArchiveFilter) =>
  queryOptions({
    queryKey: keys.archive(f),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/archive", {
          params: {
            query: { group: f.group, q: f.q, reason: f.reason, used: f.used, sort: f.sort, page: f.page, limit: f.size },
          },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
  })

/** IssueFilter is a list of open issues (GET /issues): its scope, state, kind, whose and order, and the page. */
export interface IssueFilter {
  group?: string
  repo?: string
  state?: Schemas["IssueState"]
  kind?: Schemas["IssueKind"]
  by?: "people" | "automation"
  sort?: Schemas["QueueSort"]
  page?: number
  size?: number
}

/** issuesQuery is one page of open issues, what waits first; /issues and the repository's panel read it. */
export const issuesQuery = (api: Api, f: IssueFilter) =>
  queryOptions({
    queryKey: keys.issues(f),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/issues", {
          params: {
            query: {
              group: f.group,
              repo: f.repo,
              state: f.state,
              kind: f.kind,
              by: f.by,
              sort: f.sort,
              page: f.page,
              limit: f.size,
            },
          },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
  })

/** pullsQuery is one page of open pull requests, whose turn first; the pages, the dashboard, groups and repositories read it. */
export const pullsQuery = (api: Api, f: PullFilter) =>
  queryOptions({
    queryKey: keys.pulls(f),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/pulls", {
          params: {
            query: {
              group: f.group,
              repo: f.repo,
              state: f.state,
              by: f.by,
              drafts: f.drafts,
              sort: f.sort,
              page: f.page,
              limit: f.size,
            },
          },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
  })
