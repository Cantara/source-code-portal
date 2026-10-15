import { queryOptions } from "@tanstack/react-query"
import type { Get } from "@/lib/api/client"

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
