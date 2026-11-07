"use client"

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { createContext, useContext, useDeferredValue, useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api/client"
import { pullsQuery, repoQuery } from "@/lib/api/queries"
import { stateOf, type Pull } from "@/entities/pull/pull"
import { useSnapshotVersion } from "@/lib/api/snapshot"
import { count, exactUTC } from "@/lib/format"
import { firstPageOf, rememberSize } from "@/lib/paging"
import { AiMark } from "@/entities/commit/ai-mark"
import { Author } from "@/entities/commit/author"
import { GraphCell, maxLanes } from "@/entities/commit/graph-cell"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { crumbs } from "@/lib/crumbs"
import { PageHeader } from "@/shared/page-header"
import { Pager } from "@/shared/pager"
import { Choices, FilterForm, FilterGroup, FilterLayout, SizeChoices, TextFilter, Toggle } from "@/shared/filters"
import type { Filters } from "@/lib/preferences"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { DiffStat, said } from "@/shared/diffstat"
import { historyQuery, historyState, request, search, type HistoryCommit, type HistoryState, type Label } from "./history"

const searchId = "history-search"

/** NavigatorView is a repository's history as a git graph in numbered pages: filters, the rows, the selected commit's details. */
export function NavigatorView({
  owner,
  name,
  defaultSize,
  filters,
}: {
  owner: string
  name: string
  defaultSize: number
  filters: Filters
}) {
  // The state of the address, made again only when the address changes: the effects below depend on its filter.
  const path = usePathname()
  const query = useSearchParams().toString()
  const st = useMemo(() => historyState(Object.fromEntries(new URLSearchParams(query)), defaultSize), [query, defaultSize])
  const detail = useQuery(repoQuery(api, owner, name)).data
  const repo = detail?.repo
  const open = useQuery(pullsQuery(api, { repo: `${owner}/${name}`.toLowerCase(), drafts: true, size: 100 })).data
  const heads = useMemo(() => new Map((open?.pulls ?? []).filter((p) => !p.fork).map((p) => [p.head, p])), [open])
  return (
    <PullHeads value={heads}>
      <PageHeader
        title={`Commits in ${repo ? `${repo.owner}/${repo.name}` : `${owner}/${name}`}`}
        trail={crumbs.repoCommits(repo?.owner ?? owner, repo?.name ?? name, detail?.group)}
      />
      <FilterLayout
        initial={filters}
        label="Filter commits"
        clear={search({ filter: st.filter, size: defaultSize }, defaultSize) === "" ? undefined : path}
        filters={<HistoryFilters owner={owner} name={name} st={st} defaultSize={defaultSize} />}
      >
        <Navigator
          key={search({ filter: st.filter, size: defaultSize }, defaultSize)}
          owner={owner}
          name={name}
          st={st}
          defaultSize={defaultSize}
          github={repo?.htmlUrl}
        />
      </FilterLayout>
    </PullHeads>
  )
}

/** PullHeads are the repository's open pull requests by their branch, so a branch's label names its pull request. */
const PullHeads = createContext<Map<string, Pull>>(new Map())

function Navigator({
  owner,
  name,
  st,
  defaultSize,
  github,
}: {
  owner: string
  name: string
  st: HistoryState
  defaultSize: number
  github?: string
}) {
  const qc = useQueryClient()
  const path = usePathname()
  const r = request(st)
  const options = historyQuery(api, qc, owner, name, st.filter, st.size, r)
  const { data, error, isPlaceholderData, refetch } = useQuery(options)
  const version = useSnapshotVersion()
  const commits = useMemo(() => data?.commits ?? [], [data])
  // The rows are drawn from a deferred copy of the page, so React renders up to 200 of them in slices that yield to input instead
  // of one long task; the keys and the selection act on the page itself, so no key is lost while the rows catch up.
  const drawn = useDeferredValue(data)
  const index = useMemo(() => new Map(commits.map((c, i) => [c.sha, i])), [commits])
  const children = useMemo(() => {
    const m = new Map<string, string[]>()
    for (const c of commits) for (const p of c.parents) m.set(p, [...(m.get(p) ?? []), c.sha])
    return m
  }, [commits])
  const sha = st.sha
  const found = sha === undefined ? -1 : commits.findIndex((c) => c.sha.startsWith(sha))
  const selected = found >= 0 ? found : undefined
  const [open, setOpen] = useState(sha !== undefined)
  const list = useRef<HTMLDivElement>(null)
  const number = data?.page.number ?? r.page

  // The page `at` found is the page by its number too: going to it later reads no more.
  useEffect(() => {
    if (data && r.at && !isPlaceholderData)
      qc.setQueryData(historyQuery(api, qc, owner, name, st.filter, st.size, { page: data.page.number }).queryKey, data)
  }, [data, r.at, isPlaceholderData, qc, owner, name, st.filter, st.size])

  // A commit `at` found is scrolled to, once.
  const shown = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (selected !== undefined && sha !== undefined && r.at && shown.current !== sha) {
      shown.current = sha
      document.getElementById(`commit-${commits[selected]?.sha}`)?.scrollIntoView({ block: "center" })
    }
  }, [selected, sha, r.at, commits])

  // "/" focuses the search.
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      const t = e.target as HTMLElement | null
      if (e.key !== "/" || e.ctrlKey || e.metaKey || e.altKey || t?.closest("input, textarea, select")) return
      e.preventDefault() // the header's search box sees it taken
      document.getElementById(searchId)?.focus()
    }
    // Captured: this page's search is asked before the header's.
    document.addEventListener("keydown", onKey, true)
    return () => document.removeEventListener("keydown", onKey, true)
  }, [])

  // The URL is the state: filters and pages are pushed (back returns to them), the selection replaced; no server round trip.
  function write(next: HistoryState, push: boolean) {
    const url = path + search(next, defaultSize)
    if (push) window.history.pushState(null, "", url)
    else window.history.replaceState(null, "", url)
  }

  // The selection keys move from: the last one chosen, until the URL has caught up with it.
  const moving = useRef<number | undefined>(undefined)
  useEffect(() => {
    if (moving.current === selected) moving.current = undefined
  }, [selected])
  function select(i: number, panel?: boolean) {
    const c = commits[i]
    if (!c) return
    moving.current = i
    write({ ...st, page: number, sha: c.sha }, false)
    document.getElementById(`commit-${c.sha}`)?.scrollIntoView({ block: "nearest" })
    if (panel !== undefined) setOpen(panel)
  }
  function selectSha(s: string) {
    const i = index.get(s)
    if (i !== undefined) select(i, true)
    else write({ ...st, page: undefined, sha: s }, true) // on another page: the API finds it
  }

  // Going to another page; a keyboard move across the edge selects the first or last row once it loads.
  const edge = useRef<"first" | "last" | undefined>(undefined)
  function goPage(n: number, to?: "first" | "last") {
    edge.current = to
    moving.current = undefined
    write({ ...st, page: n, sha: undefined }, true)
    if (!to) list.current?.scrollIntoView({ block: "start" })
  }
  useEffect(() => {
    if (!edge.current || isPlaceholderData || commits.length === 0) return
    const i = edge.current === "first" ? 0 : commits.length - 1
    edge.current = undefined
    select(i)
  })

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const n = commits.length
    if (n === 0 || !data) return
    const i = moving.current ?? selected ?? -1
    const down = e.key === "ArrowDown" || e.key === "j"
    const up = e.key === "ArrowUp" || e.key === "k"
    if (down || up || e.key === "Home" || e.key === "End") e.preventDefault()
    if (down && i >= n - 1 && number < data.page.pages) goPage(number + 1, "first")
    else if (up && i === 0 && number > 1) goPage(number - 1, "last")
    else if (down) select(Math.min(n - 1, i + 1))
    else if (up) select(Math.max(0, i - 1))
    else if (e.key === "Home") select(0)
    else if (e.key === "End") select(n - 1)
    else if (e.key === "Enter" && selected !== undefined) {
      e.preventDefault()
      setOpen(true)
    } else if (e.key === "Escape") setOpen(false)
  }

  if (error && !data) return <ProblemAlert error={error} />
  if (!data) return <p className="text-muted-foreground">Loading commits…</p>
  const current = selected === undefined ? undefined : commits[selected]
  const drawnPage = drawn ?? data
  const currentRow = current?.graph
  const lane = currentRow ? { lane: Math.min(currentRow.lane, maxLanes - 1), color: currentRow.color } : undefined
  const missing = sha !== undefined && r.at !== undefined && !isPlaceholderData && data.at === null
  const newer = data.page.number > 1 && data.version !== 0 && version !== 0 && version !== data.version
  const href = (n: number) => path + search({ ...st, page: n, sha: undefined }, defaultSize)

  return (
    <section aria-label="History" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <FreshnessBadge freshness={data.freshness} />
        {data.filtered && (
          <span>
            {count(data.page.total)} {data.page.total === 1 ? "commit matches" : "commits match"}, shown as a list
          </span>
        )}
        {newer && (
          <Button variant="outline" onClick={() => void refetch()}>
            New commits
          </Button>
        )}
      </div>
      <Pager page={data.page} href={href} onPage={(n) => goPage(n)} />
      {missing && (
        <Alert>
          Commit <span className="font-mono">{sha.slice(0, 7)}</span> is not in the stored history: it is older, or on a branch
          the portal does not keep.{" "}
          {github && (
            <a href={`${github}/commit/${sha}`} className="underline" rel="noreferrer">
              Open it on GitHub
            </a>
          )}
        </Alert>
      )}
      <div className={`grid gap-4 ${open && current ? "lg:grid-cols-[minmax(0,1fr)_22rem]" : ""}`}>
        <div className="flex min-w-0 flex-col gap-3">
          {commits.length === 0 ? (
            <p className="text-muted-foreground">
              {data.page.total === 0 ? (
                "No commits match."
              ) : (
                <>
                  Page {data.page.number} is past the last.{" "}
                  <a
                    href={href(data.page.pages)}
                    onClick={(e) => (e.preventDefault(), goPage(data.page.pages))}
                    className="underline"
                  >
                    Go to page {data.page.pages}
                  </a>
                </>
              )}
            </p>
          ) : (
            <div
              ref={list}
              role="listbox"
              aria-label="Commits"
              aria-busy={isPlaceholderData}
              tabIndex={0}
              aria-activedescendant={current ? `commit-${current.sha}` : undefined}
              onKeyDown={onKeyDown}
              className={`scroll-mt-4 rounded-md border border-border focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none ${isPlaceholderData ? "opacity-60" : ""}`}
            >
              {drawnPage.commits.map((c, i) => (
                <Row
                  key={c.sha}
                  commit={c}
                  pos={(drawnPage.page.number - 1) * drawnPage.page.size + i + 1}
                  size={drawnPage.page.total}
                  lanes={drawnPage.lanes}
                  lane={lane}
                  selected={c.sha === current?.sha}
                  onSelect={() => selectSha(c.sha)}
                />
              ))}
            </div>
          )}
          <Pager page={data.page} href={href} onPage={(n) => goPage(n)} label="Pages, at the end" />
          {error && <ProblemAlert error={error} />}
        </div>
        {open && current && (
          <Details
            commit={current}
            known={index}
            childShas={children.get(current.sha) ?? []}
            github={github}
            onSelect={selectSha}
            onClose={() => {
              setOpen(false)
              list.current?.focus()
            }}
          />
        )}
      </div>
    </section>
  )
}

/**
 * HistoryFilters are the history's filters in the panel at the left: the branch or tag (every one listed, from the labels of the
 * page that is read), the author, the search (press /), automation and the page size. The page's read is the navigator's own, the
 * same query, so it is asked once.
 */
function HistoryFilters({
  owner,
  name,
  st,
  defaultSize,
}: {
  owner: string
  name: string
  st: HistoryState
  defaultSize: number
}) {
  const qc = useQueryClient()
  const path = usePathname()
  const router = useRouter()
  const { data } = useQuery(historyQuery(api, qc, owner, name, st.filter, st.size, request(st)))
  const f = st.filter
  const pick = (patch: Record<string, string>) => {
    const fields = { ref: f.ref, author: f.author, q: f.q, automation: f.automation === false ? "false" : undefined, ...patch }
    router.push(path + search({ filter: historyState(fields, st.size).filter, size: st.size }, defaultSize))
  }
  const pickSize = (n: number) => {
    rememberSize("history", n)
    const number = data?.page.number ?? st.page ?? 1
    window.history.pushState(
      null,
      "",
      path + search({ ...st, size: n, page: firstPageOf(number, st.size, n), sha: undefined }, defaultSize),
    )
  }
  const labels = data?.labels ?? []
  const refs = [
    { value: "", label: "All branches" },
    ...labels.map((l) => ({
      value: l.name,
      label: l.kind === "tag" ? `Tag ${l.name}` : l.default ? `${l.name} (default)` : l.name,
    })),
  ]
  if (f.ref && !refs.some((o) => o.value === f.ref)) refs.push({ value: f.ref, label: f.ref })
  return (
    <FilterForm action={path} label="Filter commits">
      <FilterGroup title="Branch or tag">
        <Choices name="ref" label="Branch or tag" value={f.ref ?? ""} onPick={(ref) => pick({ ref })} options={refs} scroll />
      </FilterGroup>
      <FilterGroup title="Author">
        <TextFilter name="author" label="Author" value={f.author ?? ""} onPick={(author) => pick({ author })} />
      </FilterGroup>
      <FilterGroup title="Search (press /)">
        <TextFilter id={searchId} name="q" label="Search the commits" value={f.q ?? ""} onPick={(q) => pick({ q })} />
      </FilterGroup>
      <FilterGroup title="Show">
        <Toggle
          name="automation"
          value="false"
          label="Hide automation"
          checked={f.automation === false}
          onPick={(on) => pick({ automation: on ? "false" : "" })}
        />
      </FilterGroup>
      <SizeChoices value={st.size} onPick={pickSize} />
    </FilterForm>
  )
}

function Row({
  commit: c,
  pos,
  size,
  lanes,
  lane,
  selected,
  onSelect,
}: {
  commit: HistoryCommit
  pos: number
  size: number
  lanes: number
  lane?: { lane: number; color: number }
  selected: boolean
  onSelect: () => void
}) {
  const said = [
    c.parents.length > 1 ? `Merge of ${c.parents.length} parents.` : "",
    c.beyond.length > 0 ? "A parent is older than the stored history." : "",
    c.automated ? "By automation." : "",
  ]
    .filter(Boolean)
    .join(" ")
  return (
    <div
      id={`commit-${c.sha}`}
      role="option"
      aria-selected={selected}
      aria-posinset={pos}
      aria-setsize={size}
      onClick={onSelect}
      className={`flex h-8 cursor-pointer scroll-my-12 items-center gap-2 px-2 text-sm [contain-intrinsic-size:auto_2rem] [content-visibility:auto] ${selected ? "bg-accent text-accent-foreground" : "hover:bg-muted"}`}
    >
      {c.graph && lanes > 0 && <GraphCell row={c.graph} lanes={lanes} height={32} nodeY={16} className="h-8" lane={lane} />}
      <span className="flex min-w-0 flex-1 items-center gap-1.5">
        <Labels labels={c.labels} row />
        <span className={`min-w-24 flex-1 truncate ${c.automated ? "text-muted-foreground" : ""}`}>{c.subject}</span>
        <AiMark commit={c} />
        {said && <span className="sr-only">{said}</span>}
      </span>
      <span className="hidden w-28 shrink-0 truncate text-muted-foreground sm:block">
        <Author name={c.authorName} login={c.authorLogin} />
      </span>
      <span className="hidden w-28 shrink-0 text-muted-foreground md:block">
        <RelativeTime at={c.committedAt} />
      </span>
      <span className="hidden w-32 shrink-0 lg:flex">
        <DiffStat stats={c.stats} />
      </span>
      <span className="w-16 shrink-0 font-mono text-muted-foreground">{c.sha.slice(0, 7)}</span>
    </div>
  )
}

/**
 * Labels are the branches and tags at a commit: in a row three, then "+N" naming the rest, on one line that yields to the subject;
 * otherwise all of them.
 */
function Labels({ labels, row = false }: { labels: Label[]; row?: boolean }) {
  if (labels.length === 0) return null
  const shown = !row || labels.length <= 3 ? labels : labels.slice(0, 3)
  const rest = labels.slice(shown.length)
  return (
    <span className={`flex items-center gap-1 ${row ? "max-w-1/2 min-w-0 shrink overflow-hidden" : "flex-wrap"}`}>
      {shown.map((l) => (
        <Pill key={l.kind + l.name} label={l} />
      ))}
      {rest.length > 0 && (
        <span
          className="shrink-0 rounded border border-border px-1 text-xs text-muted-foreground"
          title={rest.map((l) => `${l.kind} ${l.name}`).join(", ")}
        >
          +{rest.length}
          <span className="sr-only"> more: {rest.map((l) => `${l.kind} ${l.name}`).join(", ")}</span>
        </span>
      )}
    </span>
  )
}

/** Pill is a branch or a tag; a branch with an open pull request says its number and state. */
function Pill({ label: l }: { label: Label }) {
  const pull = useContext(PullHeads).get(l.name)
  const open = l.kind === "branch" ? pull : undefined
  const tone = l.kind === "tag" ? "border-dashed" : l.default ? "border-primary font-semibold" : ""
  return (
    <span
      className={`inline-flex max-w-56 shrink-0 items-center gap-1 rounded border border-border px-1 text-xs ${tone}`}
      title={open ? `Pull request #${open.number}: ${open.title} (${stateOf(open.state).label.toLowerCase()})` : undefined}
    >
      {l.kind === "tag" ? <TagIcon /> : <BranchIcon />}
      <span className="sr-only">{l.kind === "tag" ? "tag" : l.default ? "default branch" : "branch"} </span>
      <span className="truncate">{l.name}</span>
      {open && (
        <span className="inline-flex shrink-0 items-center gap-1 border-l border-border pl-1 font-mono">
          <span aria-hidden="true" className={`size-1.5 rounded-full ${stateOf(open.state).dot}`} />
          <span className="sr-only">pull request </span>#{open.number}
          <span className="sr-only">, {stateOf(open.state).label.toLowerCase()}</span>
        </span>
      )}
    </span>
  )
}

const icon = "size-3 shrink-0"

function BranchIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 12 12" className={icon} fill="none" stroke="currentColor" strokeWidth={1.4}>
      <circle cx="3" cy="2.5" r="1.5" />
      <circle cx="3" cy="9.5" r="1.5" />
      <circle cx="9" cy="3.5" r="1.5" />
      <path d="M3 4v4M9 5c0 2.5-6 1.5-6 3" />
    </svg>
  )
}

function TagIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 12 12" className={icon} fill="none" stroke="currentColor" strokeWidth={1.4}>
      <path d="M1.5 1.5h4.2l4.8 4.8-4.2 4.2-4.8-4.8z" />
      <circle cx="4" cy="4" r="0.8" fill="currentColor" stroke="none" />
    </svg>
  )
}

function Details({
  commit: c,
  known,
  childShas,
  github,
  onSelect,
  onClose,
}: {
  commit: HistoryCommit
  known: Map<string, number>
  childShas: string[]
  github?: string
  onSelect: (sha: string) => void
  onClose: () => void
}) {
  const sha = (s: string) => {
    if (known.has(s) || !c.beyond.includes(s))
      // on this page or another: selecting it finds it
      return (
        <Button variant="ghost" className="min-h-0 px-1 font-mono" onClick={() => onSelect(s)}>
          {s.slice(0, 7)}
        </Button>
      )
    return github ? (
      <a href={`${github}/commit/${s}`} className="font-mono underline" rel="noreferrer">
        {s.slice(0, 7)} (older, on GitHub)
      </a>
    ) : (
      <span className="font-mono">{s.slice(0, 7)} (older)</span>
    )
  }
  const tree = c.url.includes("/commit/") ? c.url.replace("/commit/", "/tree/") : ""
  return (
    <aside
      aria-label="Commit details"
      className="flex flex-col gap-3 self-start rounded-md border border-border p-4 text-sm lg:sticky lg:top-4"
    >
      <div className="flex items-start justify-between gap-2">
        <h2 className="font-semibold break-words">{c.subject}</h2>
        <Button variant="ghost" className="min-h-0 px-2" onClick={onClose} aria-label="Close the details">
          ✕
        </Button>
      </div>
      {c.body && <pre className="max-h-64 overflow-auto font-sans break-words whitespace-pre-wrap">{c.body}</pre>}
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1">
        <dt className="text-muted-foreground">Author</dt>
        <dd className="break-words">
          {c.authorName || c.authorLogin}
          {c.authorLogin && c.authorName ? ` (${c.authorLogin})` : ""}
          {c.automated ? ", automation" : ""}
          {c.kind === "agent" ? ", a coding agent" : ""}
        </dd>
        {c.assistant && (
          <>
            <dt className="text-muted-foreground">With AI</dt>
            <dd className="break-words">{c.assistant}</dd>
          </>
        )}
        {c.committerName && c.committerName !== c.authorName && (
          <>
            <dt className="text-muted-foreground">Committer</dt>
            <dd className="break-words">{c.committerName}</dd>
          </>
        )}
        <dt className="text-muted-foreground">Date</dt>
        <dd>
          <time dateTime={c.committedAt}>{exactUTC(c.committedAt)}</time>
        </dd>
        {c.stats && (
          <>
            <dt className="text-muted-foreground">Changes</dt>
            <dd className="flex flex-wrap items-center gap-2">
              <DiffStat stats={c.stats} />
              <span className="text-muted-foreground">{said(c.stats)}</span>
            </dd>
          </>
        )}
        <dt className="text-muted-foreground">Commit</dt>
        <dd className="font-mono break-all">{c.sha}</dd>
        <dt className="text-muted-foreground">{c.parents.length === 1 ? "Parent" : "Parents"}</dt>
        <dd className="flex flex-wrap gap-1">
          {c.parents.length === 0 ? "none" : c.parents.map((p) => <span key={p}>{sha(p)}</span>)}
        </dd>
        {childShas.length > 0 && (
          <>
            <dt className="text-muted-foreground">Children</dt>
            <dd className="flex flex-wrap gap-1">
              {childShas.map((s) => (
                <span key={s}>{sha(s)}</span>
              ))}
            </dd>
          </>
        )}
        {c.labels.length > 0 && (
          <>
            <dt className="text-muted-foreground">Refs</dt>
            <dd>
              <Labels labels={c.labels} />
            </dd>
          </>
        )}
      </dl>
      {c.url && (
        <p className="flex flex-wrap gap-3">
          <a href={c.url} className="underline" rel="noreferrer">
            The commit on GitHub
          </a>
          {tree && (
            <a href={tree} className="underline" rel="noreferrer">
              Its files
            </a>
          )}
        </p>
      )}
    </aside>
  )
}
