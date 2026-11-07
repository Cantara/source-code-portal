"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { Button } from "@/components/ui/button"
import { api, ProblemError } from "@/lib/api/client"
import { crumbs } from "@/lib/crumbs"
import type { Filters } from "@/lib/preferences"
import { Choices, FilterForm, FilterGroup, FilterLayout } from "@/shared/filters"
import { PageHeader } from "@/shared/page-header"
import { Pager } from "@/shared/pager"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { kinds, maxQuery, searchHref, searchQuery, searchState, type Hit, type SearchState } from "./api"
import { Parts } from "./parts"
import { useFeatures } from "@/lib/rules-context"

/** Problem is a refused query's problem as the server rendered it. */
export interface Problem {
  status: number
  code: string
  detail: string
  requestId: string
}

/**
 * SearchView is `/search`: the form, the kinds with their counts, the hits of the page with their snippets, and the pager; problem
 * is the server's answer to a query it refused.
 */
export function SearchView({ problem, filters }: { problem?: Problem; filters: Filters }) {
  const st = searchState(Object.fromEntries(useSearchParams()))
  return (
    <>
      <PageHeader title="Search" trail={crumbs.search()}>
        <SearchForm key={st.q} q={st.q} />
      </PageHeader>
      {st.q ? <Results st={st} problem={problem} filters={filters} /> : <Help />}
    </>
  )
}

/** SearchForm is a plain GET form, so it searches without JavaScript too. */
function SearchForm({ q }: { q: string }) {
  const router = useRouter()
  return (
    <form
      action="/search"
      role="search"
      className="flex w-full max-w-2xl gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        const value = new FormData(e.currentTarget).get("q")
        if (typeof value === "string" && value.trim())
          router.push(`/search?${new URLSearchParams({ q: value.trim() }).toString()}`)
      }}
    >
      <label htmlFor="search-q" className="sr-only">
        Search the portal
      </label>
      <input
        id="search-q"
        type="search"
        name="q"
        defaultValue={q}
        maxLength={maxQuery}
        required
        placeholder="Repositories, documents, dependencies, commits"
        className="min-h-9 min-w-0 flex-1 rounded-md border border-input bg-background px-3"
      />
      <Button type="submit">Search</Button>
    </form>
  )
}

function Results({ st, problem, filters }: { st: SearchState; problem?: Problem; filters: Filters }) {
  const router = useRouter()
  // A query the server refused is not asked again.
  const { data, error, isPending } = useQuery({ ...searchQuery(api, st), enabled: !problem })
  if (error) return <ProblemAlert error={error} />
  if (isPending && problem)
    return <ProblemAlert error={new ProblemError(problem.status, problem.code, problem.detail, problem.requestId)} />
  if (isPending) return <p aria-live="polite">Searching…</p>
  const all = Object.values(data.counts).reduce((a, b) => a + b, 0)
  const present = kinds.filter((k) => data.counts[k.kind] > 0 || st.in === k.kind)
  return (
    <FilterLayout
      initial={filters}
      label="Filter the hits"
      clear={st.in ? searchHref({ q: st.q, page: 1 }) : undefined}
      filters={
        <FilterForm action="/search" label="Filter the hits">
          <input type="hidden" name="q" value={st.q} />
          <FilterGroup title="Kind">
            <Choices
              name="in"
              label="Kind"
              value={st.in ?? ""}
              onPick={(k) => router.push(searchHref({ q: st.q, page: 1, ...(k && { in: k as NonNullable<SearchState["in"]> }) }))}
              options={[
                { value: "", label: "All", count: all },
                ...present.map((k) => ({ value: k.kind, label: k.tab, count: data.counts[k.kind] })),
              ]}
            />
          </FilterGroup>
        </FilterForm>
      }
    >
      <section aria-label="Results" className="max-w-4xl">
        {data.corrections.length > 0 && (
          <p className="mb-3 text-sm text-muted-foreground">
            Nothing holds{" "}
            {data.corrections.map((c, i) => (
              <span key={c.from}>
                {i > 0 && ", "}
                <q className="font-medium text-foreground">{c.from}</q> (searched for{" "}
                <q className="font-medium text-foreground">{c.to}</q>)
              </span>
            ))}
            .
          </p>
        )}
        {data.hits.length === 0 ? (
          <>
            <p className="mb-4">
              Nothing found for <q className="font-medium">{data.query}</q>.
            </p>
            <Help />
          </>
        ) : (
          <>
            <p className="sr-only" aria-live="polite">
              {data.page.total} hits
            </p>
            <ol className="flex flex-col gap-4">
              {data.hits.map((h) => (
                <HitItem key={h.url + h.kind} hit={h} />
              ))}
            </ol>
            <div className="mt-6">
              <Pager page={data.page} href={(n) => searchHref({ ...st, page: n })} label="Pages of hits" noun="hits" />
            </div>
          </>
        )}
      </section>
    </FilterLayout>
  )
}

function HitItem({ hit }: { hit: Hit }) {
  const kind = kinds.find((k) => k.kind === hit.kind)?.one ?? hit.kind
  return (
    <li className="min-w-0">
      <div className="flex flex-wrap items-baseline gap-x-2 text-xs text-muted-foreground">
        <span className="font-mono tracking-wide uppercase">{kind}</span>
        {hit.repo && hit.kind !== "repo" && <span>{hit.repo}</span>}
        {hit.at && <RelativeTime at={hit.at} />}
      </div>
      <Link href={hit.url} className="text-base font-medium break-words text-primary hover:underline">
        <Parts parts={hit.title} />
      </Link>
      {hit.snippet.length > 0 && (
        <p className="text-sm break-words text-muted-foreground">
          <Parts parts={hit.snippet} />
        </p>
      )}
    </li>
  )
}

/** Help is the query language, shown before a search and when nothing was found. */
function Help() {
  const features = useFeatures()
  const one = kinds
    .filter((k) => (k.kind !== "pull" || features.pulls) && (k.kind !== "issue" || features.issues))
    .map((k) => k.kind)
    .join(", ")
  const rows: [string, string][] = [
    ["session timeout", "every word, anywhere: names, documents, dependencies, packages and commits"],
    ['"session timeout"', "the words next to each other"],
    ["oauth -legacy", "without a word"],
    ["oauth OR saml", "either word"],
    ["in:doc, in:commit", `one kind: ${one}`],
    ["group:iam repo:whydah", "within a group, or repositories whose name holds a word"],
    ["lang:java topic:iam lifecycle:production", "a repository's language, topic or lifecycle"],
    ["dep:slf4j", "repositories that depend on a package"],
    ["author:ann kind:ai after:2026-10-01", "commits by a person, of a kind of work (hand, ai, automation), in a time"],
  ]
  return (
    <section aria-labelledby="search-help" className="max-w-3xl text-sm">
      <h2 id="search-help" className="mb-2 font-semibold">
        How to search
      </h2>
      <p className="mb-3 text-muted-foreground">
        The search reads what the portal holds, not source code. The last word also finds words it starts, and a word nothing
        holds is searched with one letter changed.
      </p>
      <dl className="grid gap-x-4 gap-y-1.5 sm:grid-cols-[minmax(0,16rem)_1fr]">
        {rows.map(([q, what]) => (
          <div key={q} className="contents">
            <dt>
              <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-words">{q}</code>
            </dt>
            <dd className="mb-1 text-muted-foreground sm:mb-0">{what}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
