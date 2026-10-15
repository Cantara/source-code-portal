"use client"

import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query"
import Link from "next/link"
import { useRouter, useSearchParams } from "next/navigation"
import { useEffect, useRef, useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api/client"
import { useSnapshotVersion } from "@/lib/api/snapshot"
import { groupPath, repoPath } from "@/lib/paths"
import { CommitList } from "@/entities/commit/commit-list"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"
import { groupQuery, groupsQuery, repoQuery } from "@/lib/api/queries"
import { commitFilter, commitLogQuery, type Filter } from "./api"

/** AllCommitsView is the commit log across repositories, filtered by group or repository through the URL. */
export function AllCommitsView() {
  const filter = commitFilter(Object.fromEntries(useSearchParams()))
  return (
    <>
      <PageHeader title="Commits">
        <FilterForm key={`${filter.group ?? ""}/${filter.repo ?? ""}`} filter={filter} />
      </PageHeader>
      <CommitLog filter={filter} />
    </>
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

/** RepoCommitsView is the commit log of a repository's default branch. */
export function RepoCommitsView({ owner, name }: { owner: string; name: string }) {
  const repo = useQuery(repoQuery(api, owner, name)).data?.repo
  return (
    <>
      <PageHeader title={`Commits in ${repo ? `${repo.owner}/${repo.name}` : `${owner}/${name}`}`}>
        <Link href={repoPath(owner, name)} className="text-sm hover:underline">
          Back to the repository
        </Link>
      </PageHeader>
      <CommitLog filter={{ repo: `${owner}/${name}` }} showRepo={false} />
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
    })
    router.push(q.size > 0 ? `/commits?${q}` : "/commits")
  }
  return (
    <form onSubmit={submit} action="/commits" className="flex flex-wrap items-end gap-3 text-sm" aria-label="Filter commits">
      <label className="flex flex-col">
        Group
        <select
          name="group"
          defaultValue={filter.group ?? ""}
          className="mt-1 min-h-9 rounded-md border border-input bg-background px-2"
        >
          <option value="">All groups</option>
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
      <Button type="submit" variant="outline">
        Apply
      </Button>
    </form>
  )
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
      <CommitList
        commits={pages.flatMap((p) => p.commits)}
        showRepo={showRepo}
        graph={filter.repo !== undefined}
        firstRef={first}
      />
      {hasNextPage && (
        <Button variant="outline" className="mt-3" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
          {isFetchingNextPage ? "Loading…" : "Load more"}
        </Button>
      )}
      {error && <ProblemAlert error={error} />}
    </section>
  )
}
