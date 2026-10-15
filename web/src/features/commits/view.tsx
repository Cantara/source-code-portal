"use client"

import { useQuery, useQueryClient } from "@tanstack/react-query"
import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useEffect, useRef, useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { api, type Commit, type Repo } from "@/lib/api/client"
import { useSnapshotVersion } from "@/lib/api/snapshot"
import { dayUTC } from "@/lib/format"
import { firstPageOf, rememberSize } from "@/lib/paging"
import { groupPath, repoAt, repoCommitsPath } from "@/lib/paths"
import { RelativeTime } from "@/shared/relative-time"
import { CommitList } from "@/entities/commit/commit-list"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { Pager, PageSizeField } from "@/shared/pager"
import { ProblemAlert } from "@/shared/problem-alert"
import { groupQuery, groupsQuery } from "@/lib/api/queries"
import { commitLogQuery, logSearch, logState, type LogState } from "./api"

/**
 * AllCommitsView is the timeline of commits across repositories, or of one product (`?group=`) with its repositories, filtered by
 * repository and automation through the URL, in numbered pages of the reader's size (defaultSize until chosen).
 */
export function AllCommitsView({ defaultSize }: { defaultSize: number }) {
  const st = logState(Object.fromEntries(useSearchParams()), defaultSize)
  return (
    <>
      <PageHeader title="Commits">
        <FilterForm key={logSearch({ ...st, page: 1 }, defaultSize)} st={st} defaultSize={defaultSize} />
      </PageHeader>
      {st.filter.group && <ProductRepos group={st.filter.group} />}
      <CommitLog st={st} defaultSize={defaultSize} />
    </>
  )
}

/** ProductRepos are a product's repositories, the most recently changed first, each opening its history. */
function ProductRepos({ group }: { group: string }) {
  const { data } = useQuery(groupQuery(api, group))
  if (!data) return null
  const changed = (r: Repo) => r.lastAuthoredAt ?? r.lastCommitAt ?? r.pushedAt ?? ""
  const repos = data.repos.toSorted((a, b) => changed(b).localeCompare(changed(a)))
  return (
    <section aria-label={`Repositories of ${data.name}`} className="mb-4">
      <ul className="flex flex-wrap gap-2 text-sm">
        {repos.map((r) => (
          <li key={r.address}>
            <Link
              href={repoCommitsPath(r.owner, r.name)}
              className="inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1 hover:bg-accent"
            >
              <span className="font-medium">{r.name}</span>
              <span className="text-muted-foreground">
                <RelativeTime at={changed(r) || null} />
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}

/** GroupCommitsView is the commit log of a group. */
export function GroupCommitsView({ group, defaultSize }: { group: string; defaultSize: number }) {
  const { data } = useQuery(groupQuery(api, group))
  const search = logState(Object.fromEntries(useSearchParams()), defaultSize)
  const st: LogState = { filter: { group }, page: search.page, size: search.size }
  return (
    <>
      <PageHeader title={`Commits in ${data?.name ?? group}`}>
        <Link href={groupPath(group)} className="text-sm hover:underline">
          Back to the group
        </Link>
      </PageHeader>
      <CommitLog st={st} defaultSize={defaultSize} sizeField />
    </>
  )
}

const field = "mt-1 min-h-9 rounded-md border border-input bg-background px-2"

function FilterForm({ st, defaultSize }: { st: LogState; defaultSize: number }) {
  const router = useRouter()
  const groups = useQuery(groupsQuery(api))
  const filter = st.filter
  const [repoText, setRepoText] = useState(filter.repo ?? "")
  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const g = String(form.get("group") ?? "")
    const repo = repoText.trim().toLowerCase()
    const next: LogState = {
      filter: {
        ...(g ? { group: g } : {}),
        ...(repo ? { repo } : {}),
        ...(form.get("automation") === "false" ? { automation: false } : {}),
      },
      page: 1,
      size: st.size,
    }
    router.push(`/commits${logSearch(next, defaultSize)}`)
  }
  return (
    <form onSubmit={submit} action="/commits" className="flex flex-wrap items-end gap-3 text-sm" aria-label="Filter commits">
      <label className="flex flex-col">
        Product
        <select name="group" defaultValue={filter.group ?? ""} className={field}>
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
      <label className="flex min-h-9 items-center gap-2">
        <input type="checkbox" name="automation" value="false" defaultChecked={filter.automation === false} />
        Hide automation
      </label>
      <SizeField st={st} defaultSize={defaultSize} />
      <Button type="submit" variant="outline">
        Apply
      </Button>
    </form>
  )
}

/** SizeField chooses the page size: remembered for the next visit, the first commit shown kept in view. */
function SizeField({ st, defaultSize }: { st: LogState; defaultSize: number }) {
  const path = usePathname()
  return (
    <PageSizeField
      value={st.size}
      onSize={(n) => {
        rememberSize("commits", n)
        window.history.pushState(
          null,
          "",
          path + logSearch({ ...st, size: n, page: firstPageOf(st.page, st.size, n) }, defaultSize),
        )
      }}
    />
  )
}

/** days are the commits by their day in UTC, newest first. */
function days(commits: Commit[]): [string, Commit[]][] {
  const out: [string, Commit[]][] = []
  for (const c of commits) {
    const day = c.committedAt.slice(0, 10)
    const last = out.at(-1)
    if (last?.[0] === day) last[1].push(c)
    else out.push([day, [c]])
  }
  return out
}

/**
 * CommitLog is one numbered page of the commits of st, grouped by day, with the pager above and below. The first page follows new
 * snapshots; a later page stays as it is and offers "New commits", which reloads it.
 */
export function CommitLog({
  st,
  defaultSize,
  showRepo = true,
  sizeField = false,
}: {
  st: LogState
  defaultSize: number
  showRepo?: boolean
  sizeField?: boolean
}) {
  const qc = useQueryClient()
  const path = usePathname()
  const { data, error, isPlaceholderData, refetch } = useQuery(commitLogQuery(api, qc, st))
  const version = useSnapshotVersion()
  const first = useRef<HTMLLIElement>(null)
  const top = useRef<HTMLElement>(null)
  const focusFirst = useRef(false)

  useEffect(() => {
    if (focusFirst.current && data && !isPlaceholderData) {
      focusFirst.current = false
      first.current?.focus()
    }
  }, [data, isPlaceholderData])

  function goPage(n: number) {
    window.history.pushState(null, "", path + logSearch({ ...st, page: n }, defaultSize))
    top.current?.scrollIntoView({ block: "start" })
  }

  if (error && !data) return <ProblemAlert error={error} />
  if (!data) return <p className="text-muted-foreground">Loading commits…</p>
  const newer = data.page.number > 1 && data.version !== 0 && version !== 0 && version !== data.version
  const href = (n: number) => path + logSearch({ ...st, page: n }, defaultSize)
  const past = data.commits.length === 0 && data.page.total > 0
  return (
    <section ref={top} aria-label="Commit log" aria-busy={isPlaceholderData} className="flex scroll-mt-4 flex-col gap-3">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <FreshnessBadge freshness={data.freshness} />
          {newer && (
            <Button
              variant="outline"
              onClick={() => {
                focusFirst.current = true
                void refetch()
              }}
            >
              New commits
            </Button>
          )}
        </div>
        {sizeField && (
          <form action={path} className="text-sm" aria-label="Page size" onSubmit={(e) => e.preventDefault()}>
            <SizeField st={st} defaultSize={defaultSize} />
          </form>
        )}
      </div>
      <Pager page={data.page} href={href} onPage={goPage} />
      <div className={isPlaceholderData ? "opacity-60" : undefined}>
        {days(data.commits).map(([day, commits], i) => (
          <section key={day} aria-labelledby={`day-${day}`} className="mb-4">
            <h2 id={`day-${day}`} className="mb-1 text-sm font-semibold text-muted-foreground">
              <time dateTime={day}>{dayUTC(`${day}T00:00:00Z`)}</time>
            </h2>
            <CommitList
              commits={commits}
              showRepo={showRepo}
              firstRef={i === 0 ? first : undefined}
              repoHref={(a) => repoCommitsPath(repoAt(a).owner, repoAt(a).name)}
            />
          </section>
        ))}
        {data.page.total === 0 && <CommitList commits={[]} />}
        {past && (
          <p className="text-muted-foreground">
            Page {data.page.number} is past the last.{" "}
            <a href={href(data.page.pages)} onClick={(e) => (e.preventDefault(), goPage(data.page.pages))} className="underline">
              Go to page {data.page.pages}
            </a>
          </p>
        )}
      </div>
      {data.commits.length > 0 && <Pager page={data.page} href={href} onPage={goPage} label="Pages, at the end" />}
      {error && <ProblemAlert error={error} />}
    </section>
  )
}
