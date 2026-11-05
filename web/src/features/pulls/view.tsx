"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useRef, useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api/client"
import { groupsQuery, pullsQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { crumbs } from "@/lib/crumbs"
import { count, plural } from "@/lib/format"
import { firstPageOf, rememberSize } from "@/lib/paging"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { Pager, PageSizeField } from "@/shared/pager"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { PullList, pullStates, type PullCounts, type PullState } from "@/entities/pull/pull"
import { pullsSearch, pullsState, type PullsState } from "./api"

/**
 * PullsView is every open pull request of the reader's view, whose turn first (design/pulls.md): the states as chips with their
 * counts, which filter; the product, the repository, whose, drafts and the order in a GET form that works without JavaScript; numbered
 * pages of the reader's size (defaultSize until chosen).
 */
export function PullsView({ defaultSize }: { defaultSize: number }) {
  const st = pullsState(Object.fromEntries(useSearchParams()), defaultSize)
  const path = usePathname()
  const top = useRef<HTMLElement>(null)
  const { data, error, isPlaceholderData } = useQuery(pullsQuery(api, { ...st.filter, page: st.page, size: st.size }))
  const href = (n: number) => path + pullsSearch({ ...st, page: n }, defaultSize)
  function goPage(n: number) {
    window.history.pushState(null, "", href(n))
    top.current?.scrollIntoView({ block: "start" })
  }
  return (
    <>
      <PageHeader
        title="Pull requests"
        trail={crumbs.pulls()}
        meta={data && <span className="font-mono text-xs text-muted-foreground">{plural(data.counts.open, "open", "open")}</span>}
      >
        <FilterForm key={pullsSearch({ ...st, page: 1 }, defaultSize)} st={st} defaultSize={defaultSize} />
      </PageHeader>
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <section
          ref={top}
          aria-label="Open pull requests"
          aria-busy={isPlaceholderData}
          className="flex scroll-mt-4 flex-col gap-3"
        >
          <StateChips counts={data.counts} st={st} defaultSize={defaultSize} />
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <FreshnessBadge freshness={data.freshness} />
            {data.counts.oldestReview && (
              <span>
                The person&apos;s pull request waiting longest for a review was last updated{" "}
                <RelativeTime at={data.counts.oldestReview} />.
              </span>
            )}
          </div>
          <Pager page={data.page} href={href} onPage={goPage} noun="pull requests" />
          <div className={cn("panel px-3", isPlaceholderData && "opacity-60")}>
            <PullList
              pulls={data.pulls}
              empty={st.filter.state ? "No open pull request in this state" : "No open pull requests"}
            />
          </div>
          {data.pulls.length > 0 && (
            <Pager page={data.page} href={href} onPage={goPage} noun="pull requests" label="Pages, at the end" />
          )}
          {error && <ProblemAlert error={error} />}
        </section>
      )}
    </>
  )
}

/**
 * StateChips are the states with their counts, each a link that filters to it (its count across the other filters), "All open"
 * first; the current one is marked. Drafts count among all, though the list leaves them out until asked.
 */
function StateChips({ counts, st, defaultSize }: { counts: PullCounts; st: PullsState; defaultSize: number }) {
  const path = usePathname()
  const to = (state?: PullState) => path + pullsSearch({ ...st, filter: { ...st.filter, state }, page: 1 }, defaultSize)
  const chip =
    "inline-flex min-h-8 items-center gap-1.5 rounded-full border border-border px-3 text-sm hover:bg-accent aria-[current=page]:border-primary aria-[current=page]:bg-accent aria-[current=page]:font-medium"
  return (
    <nav aria-label="By whose turn">
      <ul className="flex flex-wrap gap-2">
        <li>
          <Link href={to()} className={chip} aria-current={st.filter.state ? undefined : "page"}>
            All open <span className="font-mono tabular-nums">{count(counts.open)}</span>
          </Link>
        </li>
        {pullStates
          .filter((s) => counts.states[s.state] > 0 || st.filter.state === s.state)
          .map((s) => (
            <li key={s.state}>
              <Link
                href={to(s.state)}
                className={chip}
                aria-current={st.filter.state === s.state ? "page" : undefined}
                title={`${s.label}: ${s.waits}`}
              >
                <span aria-hidden="true" className={cn("size-2 rounded-full", s.dot)} />
                {s.label} <span className="font-mono tabular-nums">{count(counts.states[s.state])}</span>
              </Link>
            </li>
          ))}
      </ul>
    </nav>
  )
}

const field = "mt-1 min-h-9 rounded-md border border-input bg-background px-2"

function FilterForm({ st, defaultSize }: { st: PullsState; defaultSize: number }) {
  const router = useRouter()
  const path = usePathname()
  const groups = useQuery(groupsQuery(api))
  const f = st.filter
  const [repoText, setRepoText] = useState(f.repo ?? "")
  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = Object.fromEntries(new FormData(e.currentTarget))
    const next = pullsState({ ...form, repo: repoText.trim(), state: f.state, per: String(st.size) }, defaultSize)
    router.push(`/pulls${pullsSearch({ ...next, page: 1 }, defaultSize)}`)
  }
  return (
    <form onSubmit={submit} action="/pulls" className="flex flex-wrap items-end gap-3 text-sm" aria-label="Filter pull requests">
      {f.state && <input type="hidden" name="state" value={f.state} />}
      <label className="flex flex-col">
        Product
        <select name="group" defaultValue={f.group ?? ""} className={field}>
          <option value="">All products</option>
          {groups.data?.groups.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
      </label>
      <label className="flex flex-col">
        Repository (owner/name)
        <input name="repo" value={repoText} onChange={(e) => setRepoText(e.target.value)} className={field} />
      </label>
      <label className="flex flex-col">
        Opened by
        <select name="by" defaultValue={f.by ?? ""} className={field}>
          <option value="">People and automation</option>
          <option value="people">People</option>
          <option value="automation">Automation</option>
        </select>
      </label>
      <label className="flex flex-col">
        Order
        <select name="sort" defaultValue={f.sort ?? "waiting"} className={field}>
          <option value="waiting">Whose turn, waiting longest first</option>
          <option value="newest">Newest first</option>
          <option value="updated">Updated last first</option>
        </select>
      </label>
      <label className="flex min-h-9 items-center gap-2">
        <input type="checkbox" name="drafts" value="true" defaultChecked={f.drafts === true} />
        Show drafts
      </label>
      <PageSizeField
        value={st.size}
        onSize={(n) => {
          rememberSize("pulls", n)
          window.history.pushState(
            null,
            "",
            path + pullsSearch({ ...st, size: n, page: firstPageOf(st.page, st.size, n) }, defaultSize),
          )
        }}
      />
      <Button type="submit" variant="outline">
        Apply
      </Button>
    </form>
  )
}
