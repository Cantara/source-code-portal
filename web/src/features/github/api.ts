import { queryOptions } from "@tanstack/react-query"
import type { Get } from "@/lib/api/client"

/** Token is what the portal shows of a person's linked GitHub token (internal/people): never the token itself. */
export interface Token {
  source: string
  owner: string
  login: string
  expiresAt?: string
  createdAt: string
  /** ok, or why GitHub refused it last (unauthorized, forbidden, not_found). */
  state: string
  /** when its repositories were last listed; absent until the first listing. */
  listedAt?: string
}

/** Owner is one organisation the portal reads: the link that makes its token, and the token linked for it. */
export interface Owner {
  owner: string
  createUrl: string
  token: Token | null
}

/** Tokens is `GET /api/v1/me/github/tokens`: every source read with people's tokens, its organisations and the reader's tokens. */
export interface Tokens {
  sources: { source: string; webUrl: string; owners: Owner[] }[]
  permissions: string[]
}

export const tokensKey = ["me", "github", "tokens"] as const

export const tokensQuery = (get: Get) =>
  queryOptions({
    queryKey: tokensKey,
    queryFn: ({ signal }) => get<Tokens>("/me/github/tokens", signal),
  })

/** warnDays is how long before its end a token is said to end soon. */
export const warnDays = 14

/** endsSoon reports whether a token ends within warnDays of now, or has ended. */
export function endsSoon(t: Token, now: number): boolean {
  return t.expiresAt !== undefined && Date.parse(t.expiresAt) - now < warnDays * 24 * 3600 * 1000
}
