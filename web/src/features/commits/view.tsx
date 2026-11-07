"use client"

import { useQuery, useQueryClient } from "@tanstack/react-query"
import Link from "next/link"
import { usePathname } from "next/navigation"
import { useEffect, useRef } from "react"
import { Button } from "@/components/ui/button"
import { api, type Commit, type Repo } from "@/lib/api/client"
import { useSnapshotVersion } from "@/lib/api/snapshot"
import { dayUTC } from "@/lib/format"
import { groupCommitsPath, repoAt, repoCommitsPath } from "@/lib/paths"
import { RelativeTime } from "@/shared/relative-time"
import { CommitList } from "@/entities/commit/commit-list"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { crumbs } from "@/lib/crumbs"
import { PageHeader } from "@/shared/page-header"
import { Pager } from "@/shared/pager"
import { ProblemAlert } from "@/shared/problem-alert"
import { groupQuery } from "@/lib/api/queries"
import {
  FilterForm,
  FilterGroup,
  FilterLayout,
  ProductChoices,
  SizeChoices,
  TextFilter,
  Toggle,
  useListNav,
} from "@/shared/filters"
import type { Filters } from "@/lib/preferences"
import { commitLogQuery, logSearch, logState, type LogState } from "./api"

/**
 * AllCommitsView is the timeline of commits across repositories, or of one product (`?group=`) with its repositories: the filters
 * (the product, the repository, automation, the page size) are in the panel at the left, in numbered pages of the reader's size
 * (defaultSize until chosen).
 */
export function AllCommitsView({ defaultSize, filters }: { defaultSize: number; filters: Filters }) {
  const { st, pick, pickSize, clear } = useListNav({
    list: "commits",
    base: "/commits",
    parse: logState,
    serialize: logSearch,
    defaultSize,
  })
  const f = st.filter
  return (
    <>
      <PageHeader title="Commits" trail={crumbs.commits()} />
      <FilterLayout
        initial={filters}
        label="Filter commits"
        clear={clear}
        filters={
          <FilterForm action="/commits" label="Filter commits">
            <ProductChoices value={f.group ?? ""} onPick={(group) => pick({ group })} />
            <FilterGroup title="Repository">
              <TextFilter
                name="repo"
                label="Repository (owner/name)"
                value={f.repo ?? ""}
                onPick={(repo) => pick({ repo: repo.toLowerCase() })}
              />
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
        }
      >
        {f.group && <ProductRepos group={f.group} />}
        <CommitLog st={st} defaultSize={defaultSize} />
      </FilterLayout>
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

/** GroupCommitsView is the commit log of a group: its page size is the one filter, in the panel at the left like the others. */
export function GroupCommitsView({ group, defaultSize, filters }: { group: string; defaultSize: number; filters: Filters }) {
  const { data } = useQuery(groupQuery(api, group))
  const { st, pickSize } = useListNav({
    list: "commits",
    base: groupCommitsPath(group),
    parse: (fields, size) => ({ ...logState(fields, size), filter: { group } }),
    serialize: logSearch,
    defaultSize,
  })
  return (
    <>
      <PageHeader
        title={`Commits in ${data?.name ?? group}`}
        trail={crumbs.groupCommits(data?.id ?? group, data?.name ?? group)}
      />
      <FilterLayout
        initial={filters}
        label="Filter commits"
        filters={
          <FilterForm action={groupCommitsPath(group)} label="Filter commits">
            <SizeChoices value={st.size} onPick={pickSize} />
          </FilterForm>
        }
      >
        <CommitLog st={st} defaultSize={defaultSize} />
      </FilterLayout>
    </>
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
export function CommitLog({ st, defaultSize, showRepo = true }: { st: LogState; defaultSize: number; showRepo?: boolean }) {
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
      </div>
      <Pager page={data.page} href={href} onPage={goPage} />
      <div className={isPlaceholderData ? "opacity-60" : undefined}>
        {days(data.commits).map(([day, commits], i) => (
          <section key={day} aria-labelledby={`day-${day}`} className="mb-4">
            <h2 id={`day-${day}`} className="mb-1 text-sm font-semibold text-muted-foreground">
              <time dateTime={day}>{dayUTC(`${day}T00:00:00Z`)}</time>
            </h2>
            <div className="panel px-3">
              <CommitList
                commits={commits}
                showRepo={showRepo}
                firstRef={i === 0 ? first : undefined}
                repoHref={(a) => repoCommitsPath(repoAt(a).owner, repoAt(a).name)}
              />
            </div>
          </section>
        ))}
        {data.page.total === 0 && (
          <div className="panel px-3">
            <CommitList commits={[]} />
          </div>
        )}
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
