"use client"

import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query"
import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useEffect, useRef, useState, type FormEvent } from "react"
import { Button, buttonClass } from "@/components/ui/button"
import { api, type Commit, type Repo } from "@/lib/api/client"
import { useSnapshotVersion } from "@/lib/api/snapshot"
import { dayUTC } from "@/lib/format"
import { groupPath, repoAt, repoCommitsPath } from "@/lib/paths"
import { RelativeTime } from "@/shared/relative-time"
import { CommitList } from "@/entities/commit/commit-list"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"
import { groupQuery, groupsQuery } from "@/lib/api/queries"
import { commitFilter, commitLogQuery, logSearch, type Filter } from "./api"

/**
 * AllCommitsView is the timeline of commits across repositories, or of one product (`?group=`) with its repositories, filtered by
 * repository and automation through the URL.
 */
export function AllCommitsView() {
  const filter = commitFilter(Object.fromEntries(useSearchParams()))
  return (
    <>
      <PageHeader title="Commits">
        <FilterForm key={logSearch(filter)} filter={filter} />
      </PageHeader>
      {filter.group && <ProductRepos group={filter.group} />}
      <CommitLog filter={filter} />
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
export function GroupCommitsView({ group }: { group: string }) {
  const { data } = useQuery(groupQuery(api, group))
  return (
    <>
      <PageHeader title={`Commits in ${data?.name ?? group}`}>
        <Link href={groupPath(group)} className="text-sm hover:underline">
          Back to the group
        </Link>
      </PageHeader>
      <CommitLog filter={{ group }} />
    </>
  )
}

function FilterForm({ filter }: { filter: Filter }) {
  const router = useRouter()
  const groups = useQuery(groupsQuery(api))
  const [repoText, setRepoText] = useState(filter.repo ?? "")
  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const g = String(form.get("group") ?? "")
    const q = new URLSearchParams({
      ...(g ? { group: g } : {}),
      ...(repoText.trim() ? { repo: repoText.trim().toLowerCase() } : {}),
      ...(form.get("automation") === "false" ? { automation: "false" } : {}),
    })
    router.push(q.size > 0 ? `/commits?${q}` : "/commits")
  }
  return (
    <form onSubmit={submit} action="/commits" className="flex flex-wrap items-end gap-3 text-sm" aria-label="Filter commits">
      <label className="flex flex-col">
        Product
        <select
          name="group"
          defaultValue={filter.group ?? ""}
          className="mt-1 min-h-9 rounded-md border border-input bg-background px-2"
        >
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
        <input
          name="repo"
          value={repoText}
          onChange={(e) => setRepoText(e.target.value)}
          className="mt-1 min-h-9 rounded-md border border-input bg-background px-2"
        />
      </label>
      <label className="flex min-h-9 items-center gap-2">
        <input type="checkbox" name="automation" value="false" defaultChecked={filter.automation === false} />
        Hide automation
      </label>
      <Button type="submit" variant="outline">
        Apply
      </Button>
    </form>
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

/** CommitLog pages through the commits of the filter with "Load more"; on a new snapshot with several pages loaded it offers "New commits". */
export function CommitLog({ filter, showRepo = true }: { filter: Filter; showRepo?: boolean }) {
  const qc = useQueryClient()
  const options = commitLogQuery(api, qc, filter)
  const { data, error, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery(options)
  const version = useSnapshotVersion()
  const first = useRef<HTMLLIElement>(null)
  const focusFirst = useRef(false)
  const pages = data?.pages ?? []
  const top = pages[0]
  const path = usePathname()
  const nextCursor = pages.at(-1)?.page.nextCursor ?? undefined
  const newer = pages.length > 1 && top !== undefined && top.version !== 0 && version !== 0 && version !== top.version

  const loaded = data?.pages.length ?? 0
  useEffect(() => {
    if (focusFirst.current && loaded === 1) {
      focusFirst.current = false
      first.current?.focus()
    }
  }, [loaded, data])

  function reload() {
    focusFirst.current = true
    void qc.resetQueries({ queryKey: options.queryKey })
  }

  if (error && !data) return <ProblemAlert error={error} />
  if (!data || !top) return <p className="text-muted-foreground">Loading commits…</p>
  return (
    <section aria-label="Commit log">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <FreshnessBadge freshness={top.freshness} />
        {newer && (
          <Button variant="outline" onClick={reload}>
            New commits
          </Button>
        )}
      </div>
      {days(pages.flatMap((p) => p.commits)).map(([day, commits], i) => (
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
      {pages.every((p) => p.commits.length === 0) && <CommitList commits={[]} />}
      {hasNextPage && nextCursor && (
        <a
          href={path + logSearch({ ...filter, cursor: nextCursor })}
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
      {filter.cursor && (
        <a href={path + logSearch({ ...filter, cursor: undefined })} className="mt-3 ml-3 inline-block hover:underline">
          Newest commits
        </a>
      )}
      {error && <ProblemAlert error={error} />}
    </section>
  )
}
