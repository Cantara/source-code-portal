"use client"

import { queryOptions, useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect, useId, useState } from "react"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardTitle } from "@/components/ui/card"
import { get, ProblemError, send } from "@/lib/api/client"

/** Indexing is what `GET /api/v1/admin/indexing` answers, outside the contract (design/indexing.md): what is kept out of search engines, with the public products and their repositories to choose from. */
export type Indexing = {
  groups: string[]
  repos: string[]
  exclude: string[]
  include: string[]
  /** Each public product with its repositories; why is "" (listed), "named", "product" or "pattern". */
  tree: { id: string; name: string; excluded: boolean; repos: RepoItem[] }[]
  ungrouped: RepoItem[]
  /** How many public repositories each pattern matches, in the order of the lists. */
  excludeMatches: number[]
  includeMatches: number[]
  /** Private repositories: never offered to search engines, never listed here. */
  private: number
  revision: string
  writable: boolean
  problem: string
}
export type RepoItem = { address: string; why: "" | "named" | "product" | "pattern" }

/** Draft is the list being edited: the patterns as the lines of two text boxes. */
export type Draft = { groups: string[]; repos: string[]; exclude: string; include: string }

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

/** lines are a text box's patterns as the portal reads them: trimmed, without blanks or repeats, in order. */
export function lines(text: string): string[] {
  const out: string[] = []
  for (const l of text.split("\n")) {
    const t = l.trim()
    if (t && !out.includes(t)) out.push(t)
  }
  return out
}

/** matches says whether a repository's address contains the filter's words, each of them. */
export function matches(address: string, filter: string): boolean {
  const hay = address.toLowerCase()
  return filter
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w))
}

const reasons = { named: "", product: "with its product", pattern: "by a pattern" } as const

/**
 * IndexingCard is what the admin keeps out of search engines (design/indexing.md): public products with their repositories to tick
 * off, and rules (regular expressions) that keep out the repositories that arrive later, with include rules as the exceptions.
 * Private repositories are never offered to search engines and are not listed. What is kept out is left out of the sitemap and served
 * with noindex; the portal's own pages show it as before. Saving sends the whole list with the revision it was read at; someone else's
 * save in between is refused. A change is previewed by the portal (a dry run) before it is saved.
 */
export function IndexingCard() {
  const qc = useQueryClient()
  const { data, error: loadError, refetch } = useQuery(indexingQuery())
  const [draft, setDraft] = useState<Draft | null>(null)
  const [preview, setPreview] = useState<Indexing | null>(null)
  const [problem, setProblem] = useState("")
  const [filter, setFilter] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ProblemError | null>(null)
  const titleId = useId()
  const filterId = useId()
  const excludeId = useId()
  const includeId = useId()
  const revision = data?.revision
  useEffect(() => {
    if (!draft) return
    const ctl = new AbortController()
    const t = setTimeout(() => {
      send<Indexing>("PUT", path, {
        dryRun: true,
        groups: draft.groups,
        repos: draft.repos,
        exclude: lines(draft.exclude),
        include: lines(draft.include),
      })
        .then((p) => (ctl.signal.aborted ? undefined : (setPreview(p), setProblem(""))))
        .catch((e) => (ctl.signal.aborted ? undefined : setProblem(e instanceof Error ? e.message : String(e))))
    }, 300)
    return () => {
      clearTimeout(t)
      ctl.abort()
    }
  }, [draft, revision])
  if (loadError) return <Alert variant="error">The list could not be read: {loadError.message}</Alert>
  if (!data) return null
  const now: Draft = draft ?? {
    groups: data.groups,
    repos: data.repos,
    exclude: data.exclude.join("\n"),
    include: data.include.join("\n"),
  }
  const view = draft ? (preview ?? data) : data
  const dirty = draft !== null
  const reset = () => (setDraft(null), setPreview(null), setProblem(""))
  const edit = (change: Partial<Draft>) => setDraft({ ...now, ...change })
  const save = async () => {
    setBusy(true)
    setError(null)
    try {
      const next = await send<Indexing>("PUT", path, {
        revision: data.revision,
        groups: now.groups,
        repos: now.repos,
        exclude: lines(now.exclude),
        include: lines(now.include),
      })
      qc.setQueryData(indexingQuery().queryKey, next)
      reset()
    } catch (e) {
      setError(e instanceof ProblemError ? e : new ProblemError(0, "network", String(e)))
    } finally {
      setBusy(false)
    }
  }
  /** kept is whether the repository's box is ticked: named in the draft, or kept out some other way the portal's preview tells. */
  const kept = (r: RepoItem) => now.repos.includes(r.address) || (r.why !== "" && r.why !== "named")
  const repoRow = (r: RepoItem, key: string) => (
    <li key={key}>
      <label className="flex flex-wrap items-center gap-2 py-1 text-sm">
        <input
          type="checkbox"
          checked={kept(r)}
          disabled={r.why === "product" || (r.why === "pattern" && !now.repos.includes(r.address))}
          onChange={(e) => edit({ repos: toggled(now.repos, r.address, e.target.checked) })}
        />
        <span className="font-mono text-xs">{r.address}</span>
        {r.why !== "" && r.why !== "named" && <span className="text-xs text-muted-foreground">kept out {reasons[r.why]}</span>}
      </label>
    </li>
  )
  const matched = (r: RepoItem) => matches(r.address, filter)
  const rule = (id: string, label: string, hint: string, value: string, counts: number[], on: (v: string) => void) => (
    <div className="mt-3">
      <label htmlFor={id} className="block text-sm font-medium">
        {label}
      </label>
      <p className="text-xs text-muted-foreground">{hint}</p>
      <textarea
        id={id}
        rows={3}
        value={value}
        onChange={(e) => on(e.target.value)}
        spellCheck={false}
        placeholder="^acme/legacy-"
        className="mt-1 w-full max-w-xl rounded-md border border-border bg-background px-2 py-1 font-mono text-xs"
      />
      <ul className="text-xs text-muted-foreground">
        {lines(value).map((l, i) => (
          <li key={l}>
            <code>{l}</code>:{" "}
            {counts[i] === undefined ? "…" : `${counts[i]} public ${counts[i] === 1 ? "repository" : "repositories"}`}
          </li>
        ))}
      </ul>
    </div>
  )
  return (
    <Card className="lg:col-span-2" aria-labelledby={titleId}>
      <CardTitle id={titleId}>Search engines</CardTitle>
      <p className="mt-1 text-sm text-muted-foreground">
        Keep products and repositories out of search engines such as Google: they are left out of the sitemap and their pages say
        noindex. The portal itself still shows them. Private repositories are never offered to search engines and are not listed
        {data.private > 0 ? ` (${data.private} on this portal)` : ""}. A search engine needs a few days to drop a page it already
        holds.
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
      {problem && (
        <Alert variant="error" className="mt-3">
          {problem}
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
        <legend className="text-sm font-medium">Rules for repositories now and later</legend>
        {rule(
          excludeId,
          "Keep out the repositories whose address matches",
          "One regular expression per line, matched against owner/name, not case-sensitive. A repository that arrives later is kept out the day it matches.",
          now.exclude,
          view.excludeMatches,
          (v) => edit({ exclude: v }),
        )}
        {rule(
          includeId,
          "Except the repositories whose address matches",
          "One per line. An exception wins over a rule above, not over a product or repository ticked below.",
          now.include,
          view.includeMatches,
          (v) => edit({ include: v }),
        )}
      </fieldset>
      <fieldset className="mt-4" disabled={!data.writable || busy}>
        <legend className="text-sm font-medium">Public products and their repositories</legend>
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
          {view.tree.length === 0 && view.ungrouped.length === 0 && (
            <p className="text-sm text-muted-foreground">No public products or repositories yet.</p>
          )}
          {view.tree.map((g) => {
            const repos = g.repos.filter(matched)
            if (filter && repos.length === 0) return null
            return (
              <div key={g.id} className="border-b border-border py-1">
                <label className="flex items-center gap-2 py-1 text-sm font-medium">
                  <input
                    type="checkbox"
                    checked={now.groups.includes(g.id)}
                    onChange={(e) => edit({ groups: toggled(now.groups, g.id, e.target.checked) })}
                  />
                  <span>{g.name}</span>
                  <span className="text-xs font-normal text-muted-foreground">
                    keep the product out · {g.repos.length} {g.repos.length === 1 ? "repository" : "repositories"}
                  </span>
                </label>
                <ul className="ml-6">{repos.map((r) => repoRow(r, g.id + ":" + r.address))}</ul>
              </div>
            )
          })}
          {view.ungrouped.filter(matched).length > 0 && (
            <div className="py-1">
              <p className="py-1 text-sm font-medium">In no product</p>
              <ul className="ml-6">{view.ungrouped.filter(matched).map((r) => repoRow(r, ":" + r.address))}</ul>
            </div>
          )}
        </div>
      </fieldset>
      <div className="mt-4 flex flex-wrap gap-2">
        <Button onClick={() => void save()} disabled={!dirty || busy || !data.writable || problem !== ""}>
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
