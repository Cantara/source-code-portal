/**
 * Typed builders for URLs the Go server answers (not SPA routes). The only file besides routes.ts allowed to contain
 * path literals (eslint no-restricted-syntax). A Go test compares these outputs with routes.Table builders.
 */
const seg = (s: string) => encodeURIComponent(s)
export const serverRoutes = {
  badge: (repo: string, file: string) => `/badges/${seg(repo)}/${seg(file)}`,
  groupBadge: (groupId: string, file: string) => `/badges/groups/${seg(groupId)}/${seg(file)}`,
  readmeMd: (repo: string) => `/repos/${seg(repo)}/readme.md`,
  llms: () => "/llms.txt",
  llmsFull: () => "/llms-full.txt",
  openapi: () => "/api/v1/openapi.yaml",
} as const
