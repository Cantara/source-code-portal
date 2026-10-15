"use client"

import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query"
import { useWindowVirtualizer } from "@tanstack/react-virtual"
import Link from "next/link"
import { usePathname, useSearchParams } from "next/navigation"
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { Alert } from "@/components/ui/alert"
import { Button, buttonClass } from "@/components/ui/button"
import { api } from "@/lib/api/client"
import { repoQuery } from "@/lib/api/queries"
import { useSnapshotVersion } from "@/lib/api/snapshot"
import { count, exactUTC } from "@/lib/format"
import { repoPath } from "@/lib/paths"
import { graph, maxLanes } from "@/entities/commit/graph"
import { GraphCell } from "@/entities/commit/graph-cell"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { historyFilter, historyQuery, search, type HistoryCommit, type HistoryFilter, type Label } from "./history"

/** rowHeight is a row's height in pixels (h-8). */
const rowHeight = 32
const searchId = "history-search"

/** selectedSha is the commit the URL selects: a SHA or a prefix of at least 4 hex digits. */
function selectedSha(v: string | null): string | undefined {
  const s = v?.trim().toLowerCase()
  return s && /^[0-9a-f]{4,40}$/.test(s) ? s : undefined
}

/** NavigatorView is a repository's history as a git graph: filters, the rows, the details of the selected commit. */
export function NavigatorView({ owner, name }: { owner: string; name: string }) {
  const params = useSearchParams()
  const filter = historyFilter(Object.fromEntries(params))
  const repo = useQuery(repoQuery(api, owner, name)).data?.repo
  return (
    <>
      <PageHeader title={`Commits in ${repo ? `${repo.owner}/${repo.name}` : `${owner}/${name}`}`}>
        <Link href={repoPath(owner, name)} className="text-sm hover:underline">
          Back to the repository
        </Link>
      </PageHeader>
      <Navigator
        key={search(filter)}
        owner={owner}
        name={name}
        filter={filter}
        sha={selectedSha(params.get("sha"))}
        github={repo?.htmlUrl}
      />
    </>
  )
}

function Navigator({
  owner,
  name,
  filter,
  sha,
  github,
}: {
  owner: string
  name: string
  filter: HistoryFilter
  sha?: string
  github?: string
}) {
  const qc = useQueryClient()
  const path = usePathname()
  const options = historyQuery(api, qc, owner, name, filter)
  const { data, error, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery(options)
  const version = useSnapshotVersion()
  const pages = useMemo(() => data?.pages ?? [], [data])
  const top = pages[0]
  const commits = useMemo(() => pages.flatMap((p) => p.commits), [pages])
  const drawn = top !== undefined && !top.filtered
  const layout = useMemo(() => (drawn ? graph(commits) : undefined), [drawn, commits])
  const index = useMemo(() => new Map(commits.map((c, i) => [c.sha, i])), [commits])
  const children = useMemo(() => {
    const m = new Map<string, string[]>()
    for (const c of commits) for (const p of c.parents) m.set(p, [...(m.get(p) ?? []), c.sha])
    return m
  }, [commits])
  const at = sha === undefined ? undefined : (index.get(sha) ?? commits.findIndex((c) => c.sha.startsWith(sha)))
  const selected = at !== undefined && at >= 0 ? at : undefined
  const [open, setOpen] = useState(sha !== undefined)
  const newer = pages.length > 1 && top !== undefined && top.version !== 0 && version !== 0 && version !== top.version

  // The list's distance from the top of the page, for the window virtualizer.
  const list = useRef<HTMLDivElement>(null)
  const [margin, setMargin] = useState(0)
  useLayoutEffect(() => {
    const el = list.current
    if (el) setMargin(el.getBoundingClientRect().top + window.scrollY)
  }, [])
  const v = useWindowVirtualizer({
    count: commits.length,
    estimateSize: () => rowHeight,
    overscan: 12,
    scrollMargin: margin,
    // The first render (the server's too) draws the whole first page, so the page reads without JavaScript.
    initialRect: { width: 1024, height: rowHeight * 100 },
  })
  const items = v.getVirtualItems()
  const last = items.at(-1)
  const before = items[0] ? items[0].start - margin : 0
  const after = last ? v.getTotalSize() - (last.end - margin) : 0
  const above = useRef<HTMLDivElement>(null)
  const below = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    // Set through the DOM: the CSP allows no style attribute.
    if (above.current) above.current.style.height = `${before}px`
    if (below.current) below.current.style.height = `${after}px`
  })

  // Infinite loading once the reader scrolls near the end of the loaded rows.
  const scrolled = (v.scrollOffset ?? 0) > margin
  useEffect(() => {
    if (scrolled && last && last.index >= commits.length - 20 && hasNextPage && !isFetchingNextPage) void fetchNextPage()
  }, [scrolled, last, commits.length, hasNextPage, isFetchingNextPage, fetchNextPage])

  // A selected commit not loaded yet: page until it is, or the history ends.
  useEffect(() => {
    if (sha !== undefined && selected === undefined && hasNextPage && !isFetchingNextPage) void fetchNextPage()
  }, [sha, selected, hasNextPage, isFetchingNextPage, fetchNextPage])
  const scrolledTo = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (selected !== undefined && sha !== undefined && scrolledTo.current !== sha) {
      scrolledTo.current = sha
      v.scrollToIndex(selected, { align: "center" })
    }
  }, [selected, sha, v])

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

  // The selection keys move from: the last one chosen, until the URL has caught up with it.
  const moving = useRef<number | undefined>(undefined)
  useEffect(() => {
    if (moving.current === selected) moving.current = undefined
  }, [selected])

  function select(i: number, panel?: boolean) {
    const c = commits[i]
    if (!c) return
    moving.current = i
    scrolledTo.current = c.sha
    window.history.replaceState(null, "", path + search(filter, c.sha))
    v.scrollToIndex(i, { align: "auto" })
    if (panel !== undefined) setOpen(panel)
  }
  function selectSha(s: string) {
    const i = index.get(s)
    if (i !== undefined) select(i, true)
    else window.history.replaceState(null, "", path + search(filter, s))
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const n = commits.length
    if (n === 0) return
    const i = moving.current ?? selected ?? -1
    const move: Record<string, number> = { ArrowDown: i + 1, j: i + 1, ArrowUp: i - 1, k: i - 1, Home: 0, End: n - 1 }
    if (e.key in move) {
      e.preventDefault()
      const to = Math.max(0, Math.min(n - 1, move[e.key] ?? 0))
      select(to)
      if (to >= n - 5 && hasNextPage && !isFetchingNextPage) void fetchNextPage()
    } else if (e.key === "Enter" && selected !== undefined) {
      e.preventDefault()
      setOpen(true)
    } else if (e.key === "Escape") {
      setOpen(false)
    }
  }

  if (error && !data) return <ProblemAlert error={error} />
  if (!data || !top) return <p className="text-muted-foreground">Loading commits…</p>
  const current = selected === undefined ? undefined : commits[selected]
  const lane =
    current && layout && selected !== undefined && layout.rows[selected]
      ? { lane: Math.min(layout.rows[selected].lane, maxLanes - 1), color: layout.rows[selected].color }
      : undefined
  const missing = sha !== undefined && selected === undefined && !hasNextPage
  const nextCursor = pages.at(-1)?.page.nextCursor ?? undefined

  return (
    <section aria-label="History" className="flex flex-col gap-3">
      <FilterForm path={path} filter={filter} labels={top.labels} />
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span>
          {count(top.total)} {top.total === 1 ? "commit" : "commits"}
          {top.filtered ? " match, shown as a list" : ""}
        </span>
        <FreshnessBadge freshness={top.freshness} />
        {newer && (
          <Button variant="outline" onClick={() => void qc.resetQueries({ queryKey: options.queryKey })}>
            New commits
          </Button>
        )}
        {filter.cursor && (
          <a href={path + search({ ...filter, cursor: undefined })} className="hover:underline">
            Newest commits
          </a>
        )}
      </div>
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
        <div className="min-w-0">
          {commits.length === 0 ? (
            <p className="text-muted-foreground">No commits match.</p>
          ) : (
            <div
              ref={list}
              role="listbox"
              aria-label="Commits"
              tabIndex={0}
              aria-activedescendant={current ? `commit-${current.sha}` : undefined}
              onKeyDown={onKeyDown}
              className="rounded-md border border-border focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            >
              <div ref={above} aria-hidden="true" />
              {items.map((it) => {
                const c = commits[it.index]
                if (!c) return null
                return (
                  <Row
                    key={c.sha}
                    commit={c}
                    pos={it.index + 1}
                    size={top.total}
                    graph={layout ? { row: layout.rows[it.index], lanes: layout.lanes, lane } : undefined}
                    selected={it.index === selected}
                    onSelect={() => select(it.index, true)}
                  />
                )
              })}
              <div ref={below} aria-hidden="true" />
            </div>
          )}
          {hasNextPage && nextCursor && (
            <a
              href={path + search({ ...filter, cursor: nextCursor })}
              aria-disabled={isFetchingNextPage}
              onClick={(e) => {
                e.preventDefault()
                if (!isFetchingNextPage) void fetchNextPage()
              }}
              className={buttonClass("outline", "mt-3")}
            >
              {isFetchingNextPage ? "Loading…" : "Older commits"}
            </a>
          )}
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

function FilterForm({ path, filter, labels }: { path: string; filter: HistoryFilter; labels: Label[] }) {
  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const f = historyFilter(Object.fromEntries([...form.entries()].map(([k, v]) => [k, String(v)])))
    window.history.pushState(null, "", path + search(f))
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
      <Button type="submit" variant="outline">
        Apply
      </Button>
      {search(filter) !== "" && (
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
  graph: g,
  selected,
  onSelect,
}: {
  commit: HistoryCommit
  pos: number
  size: number
  graph?: { row: ReturnType<typeof graph>["rows"][number] | undefined; lanes: number; lane?: { lane: number; color: number } }
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
      className={`flex h-8 cursor-pointer items-center gap-2 px-2 text-sm ${selected ? "bg-accent text-accent-foreground" : "hover:bg-muted"}`}
    >
      {g?.row && <GraphCell row={g.row} lanes={g.lanes} height={32} nodeY={16} className="h-8" lane={g.lane} />}
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
