import createClient, { type Middleware } from "openapi-fetch"
import type { paths, components } from "./schema"

export type Problem = components["schemas"]["Problem"]
const etags = new Map<string, { etag: string; body: unknown }>()

const conditional: Middleware = {
  onRequest({ request }) {
    const hit = etags.get(request.url)
    if (hit) request.headers.set("If-None-Match", hit.etag)
    return request
  },
}

export const api = createClient<paths>({ baseUrl: "/" })
api.use(conditional)

export async function loadCommits(group?: string) {
  const { data, error, response } = await api.GET("/api/v1/commits", {
    params: { query: { limit: 20, ...(group ? { group } : {}) } },
  })
  if (error) throw new Error(error.title)
  if (response.status === 304) return null
  return data?.items.map((c) => c.sha.slice(0, 7)) ?? []
}

export async function loadGroup(groupId: string) {
  const { data, error } = await api.GET("/api/v1/groups/{groupId}", { params: { path: { groupId } } })
  if (error) { const p: Problem = error; return p.status }
  return data.repo_count
}
