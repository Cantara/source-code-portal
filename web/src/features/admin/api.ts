import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Get } from "@/lib/api/client"

/** AdminStatus is the operator's status (`/api/v1/admin/status`, cmd/scp/status.go): outside the contract, it evolves with operations. */
export interface AdminStatus {
  ready: boolean
  alive: boolean
  snapshot: { version: number; builtAt: string; ageSeconds: number; catalogued: boolean; repos: number }
  queue: { waiting: number; running: number }
  sources: { id: string; repos: number; freshness: string; checkedAt: string | null; rateRemaining: number }[]
  webhook: { enabled: boolean; deliveries: Record<string, number>; lastDelivery: string | null }
  cluster: { enabled: boolean; connected: boolean; leader: boolean }
  warnings: string[]
}

export const adminStatusQuery = (get: Get) =>
  queryOptions({
    queryKey: ["admin", "status"] as const,
    queryFn: ({ signal }) => get<AdminStatus>("/admin/status", signal),
    refetchInterval: 15_000,
  })

/** ProviderTest is the answer of `POST /api/v1/admin/assistant/test` (internal/assistant): one short completion. */
export interface ProviderTest {
  ok: boolean
  provider: string
  model: string
  latencyMs: number
  code?: string
  message?: string
}

/** testProvider asks the assistant's provider for one short completion. */
export async function testProvider(): Promise<ProviderTest> {
  const base = `${globalThis.location?.origin ?? "http://localhost"}/api/v1`
  return unwrap<ProviderTest>(
    globalThis
      .fetch(`${base}/admin/assistant/test`, { method: "POST", headers: { Accept: "application/json" } })
      .then(async (response) => {
        const body: unknown = await response.json().catch(() => undefined)
        return response.ok ? { data: body as ProviderTest, response } : { error: body, response }
      }),
  )
}
