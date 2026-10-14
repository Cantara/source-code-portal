import { QueryCache, QueryClient } from "@tanstack/react-query"
import { ProblemError } from "./api/client"
import { noteFailure, noteSuccess } from "./connection"

/** retryDelay honours Retry-After and otherwise backs off from one second. */
export function retryDelay(attempt: number, error: unknown): number {
  if (error instanceof ProblemError && error.retryAfter > 0) return Math.min(error.retryAfter, 60) * 1000
  return Math.min(1000 * 2 ** attempt, 8000)
}

/** createQueryClient holds the query defaults of the whole UI. */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    queryCache: new QueryCache({ onError: noteFailure, onSuccess: noteSuccess }),
    defaultOptions: {
      queries: {
        staleTime: 10_000,
        gcTime: 5 * 60_000,
        refetchOnWindowFocus: true,
        retry: (failures, error) => error instanceof ProblemError && error.retryable && failures < 2,
        retryDelay,
      },
    },
  })
}
