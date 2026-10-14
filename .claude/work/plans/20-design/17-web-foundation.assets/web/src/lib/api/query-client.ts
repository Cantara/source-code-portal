import { QueryClient } from "@tanstack/react-query"
import { isProblem, isTransient } from "./problem"

export const SWEEP_MS = 120_000
export const STALE_MS = 10_000

export function retryPolicy(failureCount: number, error: unknown): boolean {
  if (!isTransient(error)) return false
  return failureCount < 2
}
export function retryDelay(attempt: number, error: unknown): number {
  if (isProblem(error) && error.retryAfterSeconds !== null) return Math.min(error.retryAfterSeconds, 60) * 1000
  return Math.min(1000 * 2 ** attempt, 8000)
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: STALE_MS,
        gcTime: 5 * 60_000,
        retry: retryPolicy,
        retryDelay,
        refetchOnWindowFocus: true,
        refetchInterval: SWEEP_MS,
        refetchIntervalInBackground: false,
      },
    },
  })
}
