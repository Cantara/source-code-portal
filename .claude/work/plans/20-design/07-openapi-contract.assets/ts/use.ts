import createClient from "openapi-fetch"
import type { paths, components } from "./schema"

const api = createClient<paths>({ baseUrl: "/" })

type Problem = components["schemas"]["Problem"]
type Commit = components["schemas"]["Commit"]

export async function latest(): Promise<Commit[]> {
  const { data, error } = await api.GET("/api/v1/commits", { params: { query: { limit: 5, group: "platform" } } })
  if (error) throw new Error((error as Problem).code)
  return data!.items
}

export async function doc(repo: string, id: string) {
  const { data } = await api.GET("/api/v1/repos/{repo}/docs/{docId}", { params: { path: { repo, docId: id } } })
  const html: string | null | undefined = data?.html
  return html
}

export async function deps() {
  const { data } = await api.GET("/api/v1/repos/{repo}/dependencies", {
    params: { path: { repo: "billing-service" }, query: { scope: ["runtime", "dev"], relationship: "all", sort: "-name" } },
  })
  return data?.items.map((d) => d.internal?.repo ?? d.purl)
}

export async function retry() {
  return api.POST("/api/v1/admin/jobs/retry", { params: { header: { "X-SCP-Request": "1" } }, body: { key: "catalog.reconcile/_" } })
}

// @ts-expect-error limit above the contract maximum is not a number problem, but an unknown query key is
export const bad = api.GET("/api/v1/commits", { params: { query: { nope: 1 } } })
