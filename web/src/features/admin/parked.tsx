"use client"

import { queryOptions, useQuery, useQueryClient } from "@tanstack/react-query"
import { useId, useState } from "react"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardTitle } from "@/components/ui/card"
import { get, ProblemError, send } from "@/lib/api/client"
import { matches, toggled } from "./indexing"

/** Parked is what `GET /api/v1/admin/parked` answers, outside the contract (design/parked.md): the parked repositories, with every catalogued repository by product to choose from. */
export type Parked = {
  repos: string[]
  tree: { id: string; name: string; repos: ParkedItem[] }[]
  ungrouped: ParkedItem[]
  /** Parked addresses the catalogue does not hold: renamed, deleted or not listed yet. */
  missing: string[]
  revision: string
  writable: boolean
  problem: string
}
export type ParkedItem = { address: string; private: boolean; fork: boolean; parked: boolean }

const path = "/admin/parked"

export const parkedQuery = () =>
  queryOptions({
    queryKey: ["admin", "parked"] as const,
    queryFn: ({ signal }) => get<Parked>(path, signal),
    staleTime: 0,
  })

/**
 * ParkedCard is the admins' list of parked repositories (design/parked.md): a parked repository is still read from GitHub, its
 * webhook events included, and kept; it is shown nowhere (no page, list, search, feed, badge, sitemap or tool) until it is taken off the
 * list, and then it is back with everything that happened meanwhile. Saving sends the whole list with the revision it was read at;
 * someone else's save in between is refused.
 */
export function ParkedCard() {
  const qc = useQueryClient()
  const { data, error: loadError, refetch } = useQuery(parkedQuery())
  const [draft, setDraft] = useState<string[] | null>(null)
  const [filter, setFilter] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ProblemError | null>(null)
  const titleId = useId()
  const filterId = useId()
  if (loadError) return <Alert variant="error">The list could not be read: {loadError.message}</Alert>
  if (!data) return null
  const now = draft ?? data.repos
  const dirty = draft !== null
  const reset = () => setDraft(null)
  const save = async () => {
    setBusy(true)
    setError(null)
    try {
      const next = await send<Parked>("PUT", path, { revision: data.revision, repos: now })
      qc.setQueryData(parkedQuery().queryKey, next)
      reset()
    } catch (e) {
      setError(e instanceof ProblemError ? e : new ProblemError(0, "network", String(e)))
    } finally {
      setBusy(false)
    }
  }
  const row = (r: ParkedItem, key: string) => {
    const on = now.includes(r.address)
    return (
      <li key={key}>
        <label className="flex flex-wrap items-center gap-2 py-1 text-sm">
          <input type="checkbox" checked={on} onChange={(e) => setDraft(toggled(now, r.address, e.target.checked))} />
          <span className="font-mono text-xs">{r.address}</span>
          {r.private && <span className="text-xs text-muted-foreground">private</span>}
          {r.fork && <span className="text-xs text-muted-foreground">fork</span>}
          <span className={on ? "text-xs font-medium text-foreground" : "text-xs text-muted-foreground"}>
            {on ? "parked" : "shown"}
          </span>
        </label>
      </li>
    )
  }
  const matched = (r: ParkedItem) => matches(r.address, filter)
  const every = new Set([...data.tree.flatMap((g) => g.repos), ...data.ungrouped].map((r) => r.address))
  const parkedCount = [...every].filter((a) => now.includes(a)).length
  return (
    <Card className="lg:col-span-2" aria-labelledby={titleId}>
      <CardTitle id={titleId}>Parked repositories</CardTitle>
      <p className="mt-1 text-sm text-muted-foreground">
        Tick a repository to park it: when you press Save it disappears from every page, list, search, feed, badge and the
        sitemap, for everyone, you included. The portal keeps reading it from GitHub, webhook events too, so untick it later and
        it is back with everything that happened meanwhile. Use it for repositories that should have been archived on GitHub.
      </p>
      {!data.writable && (
        <Alert variant="warn" className="mt-3">
          This portal has no store to save the list in.
        </Alert>
      )}
      {data.problem && (
        <Alert variant="error" className="mt-3">
          The stored list is not served: {data.problem}
        </Alert>
      )}
      {error && (
        <Alert variant="error" className="mt-3">
          {error.message}{" "}
          {error.code === "conflict" && (
            <button type="button" className="underline" onClick={() => void refetch().then(() => (reset(), setError(null)))}>
              Load the saved list
            </button>
          )}
        </Alert>
      )}
      <fieldset className="mt-4" disabled={!data.writable || busy}>
        <legend className="text-sm font-medium">Products and their repositories: tick to park</legend>
        <p className="mt-1 text-xs text-muted-foreground">
          {parkedCount} of {every.size} repositories parked.
        </p>
        <label htmlFor={filterId} className="sr-only">
          Find a repository
        </label>
        <input
          id={filterId}
          type="search"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Find a repository"
          className="mt-1 w-full max-w-sm rounded-md border border-border bg-background px-2 py-1 text-sm"
        />
        <div className="mt-2 max-h-96 overflow-y-auto">
          {data.tree.length === 0 && data.ungrouped.length === 0 && (
            <p className="text-sm text-muted-foreground">No repositories yet.</p>
          )}
          {data.tree.map((g) => {
            const repos = g.repos.filter(matched)
            if (repos.length === 0) return null
            return (
              <div key={g.id} className="border-b border-border py-1">
                <p className="py-1 text-sm font-medium">{g.name}</p>
                <ul className="ml-6">{repos.map((r) => row(r, g.id + ":" + r.address))}</ul>
              </div>
            )
          })}
          {data.ungrouped.filter(matched).length > 0 && (
            <div className="py-1">
              <p className="py-1 text-sm font-medium">In no product</p>
              <ul className="ml-6">{data.ungrouped.filter(matched).map((r) => row(r, ":" + r.address))}</ul>
            </div>
          )}
          {data.missing.length > 0 && (
            <div className="py-1">
              <p className="py-1 text-sm font-medium">
                Parked, but not in the catalogue now (renamed, deleted or not listed yet)
              </p>
              <ul className="ml-6">
                {data.missing.map((a) => row({ address: a, private: false, fork: false, parked: true }, "missing:" + a))}
              </ul>
            </div>
          )}
        </div>
      </fieldset>
      <div className="mt-4 flex flex-wrap gap-2">
        <Button onClick={() => void save()} disabled={!dirty || busy || !data.writable}>
          Save
        </Button>
        {dirty && (
          <Button variant="ghost" onClick={reset} disabled={busy}>
            Discard changes
          </Button>
        )}
      </div>
    </Card>
  )
}
