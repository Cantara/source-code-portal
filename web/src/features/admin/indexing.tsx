"use client"

import { queryOptions, useQuery, useQueryClient } from "@tanstack/react-query"
import { useId, useState } from "react"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardTitle } from "@/components/ui/card"
import { get, ProblemError, send } from "@/lib/api/client"

/** Indexing is what `GET /api/v1/admin/indexing` answers, outside the contract (design/indexing.md): what is kept out of search engines, with the choices. */
export type Indexing = {
  groups: string[]
  repos: string[]
  groupRows: { id: string; name: string; repos: number; excluded: boolean }[]
  repoRows: { address: string; groups: string[]; excluded: boolean; via: boolean }[]
  revision: string
  writable: boolean
  problem: string
}

const path = "/admin/indexing"

export const indexingQuery = () =>
  queryOptions({
    queryKey: ["admin", "indexing"] as const,
    queryFn: ({ signal }) => get<Indexing>(path, signal),
    staleTime: 0,
  })

/** toggled is the set with id in it when on, else without it. */
export function toggled(set: string[], id: string, on: boolean): string[] {
  const rest = set.filter((s) => s !== id)
  return on ? [...rest, id].sort() : rest
}

/** matches says whether a repository's address or one of its groups contains the filter's words, each of them. */
export function matches(row: { address: string; groups: string[] }, filter: string): boolean {
  const hay = `${row.address} ${row.groups.join(" ")}`.toLowerCase()
  return filter
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w))
}

const shown = 300 // repositories drawn at once: a filter narrows the rest

/**
 * IndexingCard is what the admin keeps out of search engines (design/indexing.md): products and repositories left out of the
 * sitemap and served with noindex. The portal's own pages show everything as before. A repository that is in products that are all
 * kept out is kept out too. Saving sends the whole list with the revision it was read at; someone else's save in between is refused.
 */
export function IndexingCard() {
  const qc = useQueryClient()
  const { data, error: loadError, refetch } = useQuery(indexingQuery())
  const [draft, setDraft] = useState<{ groups: string[]; repos: string[] } | null>(null)
  const [filter, setFilter] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ProblemError | null>(null)
  const titleId = useId()
  const filterId = useId()
  if (loadError) return <Alert variant="error">The list could not be read: {loadError.message}</Alert>
  if (!data) return null
  const groups = draft?.groups ?? data.groups
  const repos = draft?.repos ?? data.repos
  const dirty = draft !== null
  const rows = data.repoRows.filter((r) => matches(r, filter))
  const save = async () => {
    setBusy(true)
    setError(null)
    try {
      const next = await send<Indexing>("PUT", path, { revision: data.revision, groups, repos })
      qc.setQueryData(indexingQuery().queryKey, next)
      setDraft(null)
    } catch (e) {
      setError(e instanceof ProblemError ? e : new ProblemError(0, "network", String(e)))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Card className="lg:col-span-2" aria-labelledby={titleId}>
      <CardTitle id={titleId}>Search engines</CardTitle>
      <p className="mt-1 text-sm text-muted-foreground">
        Products and repositories kept out of search engines such as Google: they are left out of the sitemap and their pages say
        noindex. The portal itself still shows them. A repository that is only in products kept out is kept out too. A search
        engine needs a few days to drop a page it already holds.
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
            <button
              type="button"
              className="underline"
              onClick={() => void refetch().then(() => (setDraft(null), setError(null)))}
            >
              Load the saved list
            </button>
          )}
        </Alert>
      )}
      <fieldset className="mt-4" disabled={!data.writable || busy}>
        <legend className="text-sm font-medium">Products kept out</legend>
        {data.groupRows.length === 0 ? (
          <p className="mt-1 text-sm text-muted-foreground">No products yet.</p>
        ) : (
          <ul className="mt-1 grid gap-x-6 sm:grid-cols-2">
            {data.groupRows.map((g) => (
              <li key={g.id}>
                <label className="flex items-center gap-2 py-1 text-sm">
                  <input
                    type="checkbox"
                    checked={groups.includes(g.id)}
                    onChange={(e) => setDraft({ groups: toggled(groups, g.id, e.target.checked), repos })}
                  />
                  <span>{g.name}</span>
                  <span className="text-xs text-muted-foreground">{g.repos} repositories</span>
                </label>
              </li>
            ))}
          </ul>
        )}
      </fieldset>
      <fieldset className="mt-4" disabled={!data.writable || busy}>
        <legend className="text-sm font-medium">Repositories kept out</legend>
        <label htmlFor={filterId} className="sr-only">
          Find a repository
        </label>
        <input
          id={filterId}
          type="search"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Find a repository or product"
          className="mt-1 w-full max-w-sm rounded-md border border-border bg-background px-2 py-1 text-sm"
        />
        <ul className="mt-2 max-h-80 divide-y divide-border overflow-y-auto">
          {rows.slice(0, shown).map((r) => (
            <li key={r.address}>
              <label className="flex flex-wrap items-center gap-2 py-1 text-sm">
                <input
                  type="checkbox"
                  checked={repos.includes(r.address)}
                  onChange={(e) => setDraft({ groups, repos: toggled(repos, r.address, e.target.checked) })}
                />
                <span className="font-mono text-xs">{r.address}</span>
                {r.via && !repos.includes(r.address) && !dirty && (
                  <span className="text-xs text-muted-foreground">kept out with its products</span>
                )}
              </label>
            </li>
          ))}
        </ul>
        {rows.length > shown && (
          <p className="mt-1 text-xs text-muted-foreground">{rows.length - shown} more: narrow the search.</p>
        )}
        {rows.length === 0 && <p className="mt-1 text-sm text-muted-foreground">No repository matches.</p>}
      </fieldset>
      <div className="mt-4 flex flex-wrap gap-2">
        <Button onClick={() => void save()} disabled={!dirty || busy || !data.writable}>
          Save
        </Button>
        {dirty && (
          <Button variant="ghost" onClick={() => setDraft(null)} disabled={busy}>
            Discard changes
          </Button>
        )}
      </div>
    </Card>
  )
}
