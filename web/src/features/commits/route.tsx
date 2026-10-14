import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query"
import { createLazyRoute, getRouteApi, Link } from "@tanstack/react-router"
import { useEffect, useRef, useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { useSnapshotVersion } from "@/lib/api/snapshot"
import { CommitList } from "@/shared/commit-list"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"
import { commitLogQuery, groupsQuery, type Filter } from "./api"

export const allCommitsRoute = createLazyRoute("/commits")({ component: AllCommits })
export const groupCommitsRoute = createLazyRoute("/groups/$group/commits")({ component: GroupCommits })
export const repoCommitsRoute = createLazyRoute("/repos/$owner/$name/commits")({ component: RepoCommits })

const all = getRouteApi("/commits")
const group = getRouteApi("/groups/$group/commits")
const repo = getRouteApi("/repos/$owner/$name/commits")

function AllCommits() {
  const search = all.useSearch()
  return (
    <>
      <PageHeader title="Commits">
        <FilterForm filter={search} />
      </PageHeader>
      <CommitLog filter={search} />
    </>
  )
}

function GroupCommits() {
  const { group: id } = group.useParams()
  return (
    <>
      <PageHeader title={`Commits in ${id}`}>
        <Link to="/groups/$group" params={{ group: id }} className="text-sm hover:underline">
          Back to the group
        </Link>
      </PageHeader>
      <CommitLog filter={{ group: id }} />
    </>
  )
}

function RepoCommits() {
  const { owner, name } = repo.useParams()
  return (
    <>
      <PageHeader title={`Commits in ${owner}/${name}`}>
        <Link to="/repos/$owner/$name" params={{ owner, name }} className="text-sm hover:underline">
          Back to the repository
        </Link>
      </PageHeader>
      <CommitLog filter={{ repo: `${owner}/${name}` }} showRepo={false} />
    </>
  )
}

function FilterForm({ filter }: { filter: Filter }) {
  const navigate = all.useNavigate()
  const groups = useQuery(groupsQuery)
  const [repoText, setRepoText] = useState(filter.repo ?? "")
  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const g = String(form.get("group") ?? "")
    void navigate({ search: { ...(g ? { group: g } : {}), ...(repoText.trim() ? { repo: repoText.trim().toLowerCase() } : {}) } })
  }
  return (
    <form onSubmit={submit} className="flex flex-wrap items-end gap-3 text-sm" aria-label="Filter commits">
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
  const options = commitLogQuery(qc, filter)
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
      <CommitList commits={pages.flatMap((p) => p.commits)} showRepo={showRepo} firstRef={first} />
      {hasNextPage && (
        <Button variant="outline" className="mt-3" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
          {isFetchingNextPage ? "Loading…" : "Load more"}
        </Button>
      )}
      {error && <ProblemAlert error={error} />}
    </section>
  )
}
