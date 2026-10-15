"use client"

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { usePathname, useSearchParams } from "next/navigation"
import { useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api/client"
import { repoQuery } from "@/lib/api/queries"
import { useSnapshotVersion } from "@/lib/api/snapshot"
import { count, exactUTC } from "@/lib/format"
import { firstPageOf, rememberSize } from "@/lib/paging"
import { GraphCell, maxLanes } from "@/entities/commit/graph-cell"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { crumbs } from "@/lib/crumbs"
import { PageHeader } from "@/shared/page-header"
import { Pager, PageSizeField } from "@/shared/pager"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import {
  historyQuery,
  historyState,
  request,
  search,
  type HistoryCommit,
  type HistoryFilter,
  type HistoryState,
  type Label,
} from "./history"

const searchId = "history-search"

/** NavigatorView is a repository's history as a git graph in numbered pages: filters, the rows, the selected commit's details. */
export function NavigatorView({ owner, name, defaultSize }: { owner: string; name: string; defaultSize: number }) {
  const st = historyState(Object.fromEntries(useSearchParams()), defaultSize)
  const repo = useQuery(repoQuery(api, owner, name)).data?.repo
  return (
    <>
      <PageHeader
        title={`Commits in ${repo ? `${repo.owner}/${repo.name}` : `${owner}/${name}`}`}
        trail={crumbs.repoCommits(repo?.owner ?? owner, repo?.name ?? name)}
      />
      <Navigator
        key={search({ filter: st.filter, size: defaultSize }, defaultSize)}
        owner={owner}
        name={name}
        st={st}
        defaultSize={defaultSize}
        github={repo?.htmlUrl}
      />
    </>
  )
}

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
      e.preventDefault()
      document.getElementById(searchId)?.focus()
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
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

  function setSize(n: number) {
    rememberSize("history", n)
    write({ ...st, size: n, page: firstPageOf(number, st.size, n), sha: undefined }, true)
  }

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
  const currentRow = current?.graph
  const lane = currentRow ? { lane: Math.min(currentRow.lane, maxLanes - 1), color: currentRow.color } : undefined
  const missing = sha !== undefined && r.at !== undefined && !isPlaceholderData && data.at === null
  const newer = data.page.number > 1 && data.version !== 0 && version !== 0 && version !== data.version
  const href = (n: number) => path + search({ ...st, page: n, sha: undefined }, defaultSize)

  return (
    <section aria-label="History" className="flex flex-col gap-3">
      <FilterForm
        path={path}
        st={st}
        defaultSize={defaultSize}
        labels={data.labels}
        onFilter={(f) => write({ filter: f, size: st.size }, true)}
        onSize={setSize}
      />
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
              {commits.map((c, i) => (
                <Row
                  key={c.sha}
                  commit={c}
                  pos={(data.page.number - 1) * data.page.size + i + 1}
                  size={data.page.total}
                  lanes={data.lanes}
                  lane={lane}
                  selected={i === selected}
                  onSelect={() => select(i, true)}
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

function FilterForm({
  path,
  st,
  defaultSize,
  labels,
  onFilter,
  onSize,
}: {
  path: string
  st: HistoryState
  defaultSize: number
  labels: Label[]
  onFilter: (f: HistoryFilter) => void
  onSize: (n: number) => void
}) {
  const filter = st.filter
  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    onFilter(historyState(Object.fromEntries([...form.entries()].map(([k, v]) => [k, String(v)])), st.size).filter)
  }
  const field = "mt-1 min-h-9 rounded-md border border-input bg-background px-2"
  return (
    <form onSubmit={submit} action={path} className="flex flex-wrap items-end gap-3 text-sm" aria-label="Filter commits">
      <label className="flex flex-col">
        Branch or tag
        <select name="ref" defaultValue={filter.ref ?? ""} className={field}>
          <option value="">All branches</option>
          {labels.map((l) => (
            <option key={l.kind + l.name} value={l.name}>
              {l.kind === "tag" ? `Tag ${l.name}` : l.default ? `${l.name} (default)` : l.name}
            </option>
          ))}
        </select>
      </label>
      <label className="flex flex-col">
        Author
        <input name="author" defaultValue={filter.author ?? ""} className={field} />
      </label>
      <label className="flex flex-col">
        Search (press /)
        <input id={searchId} type="search" name="q" defaultValue={filter.q ?? ""} className={field} />
      </label>
      <label className="flex min-h-9 items-center gap-2">
        <input type="checkbox" name="automation" value="false" defaultChecked={filter.automation === false} />
        Hide automation
      </label>
      <PageSizeField value={st.size} onSize={onSize} />
      <Button type="submit" variant="outline">
        Apply
      </Button>
      {search({ filter, size: defaultSize }, defaultSize) !== "" && (
        <a href={path} className="min-h-9 content-center hover:underline">
          Clear
        </a>
      )}
    </form>
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
      className={`flex h-8 cursor-pointer scroll-my-12 items-center gap-2 px-2 text-sm ${selected ? "bg-accent text-accent-foreground" : "hover:bg-muted"}`}
    >
      {c.graph && lanes > 0 && <GraphCell row={c.graph} lanes={lanes} height={32} nodeY={16} className="h-8" lane={lane} />}
      <span className="flex min-w-0 flex-1 items-center gap-1.5">
        <Labels labels={c.labels} row />
        <span className={`min-w-24 flex-1 truncate ${c.automated ? "text-muted-foreground" : ""}`}>{c.subject}</span>
        {said && <span className="sr-only">{said}</span>}
      </span>
      <span className="hidden w-36 shrink-0 truncate text-muted-foreground sm:block">{c.authorName || c.authorLogin}</span>
      <span className="hidden w-28 shrink-0 text-muted-foreground md:block">
        <RelativeTime at={c.committedAt} />
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

function Pill({ label: l }: { label: Label }) {
  const tone = l.kind === "tag" ? "border-dashed" : l.default ? "border-primary font-semibold" : ""
  return (
    <span className={`inline-flex max-w-48 shrink-0 items-center gap-1 rounded border border-border px-1 text-xs ${tone}`}>
      {l.kind === "tag" ? <TagIcon /> : <BranchIcon />}
      <span className="sr-only">{l.kind === "tag" ? "tag" : l.default ? "default branch" : "branch"} </span>
      <span className="truncate">{l.name}</span>
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
        </dd>
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
