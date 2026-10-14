"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useState } from "react"
import { Card, CardTitle } from "@/components/ui/card"
import { api, type Repo } from "@/lib/api/client"
import { plural } from "@/lib/format"
import { groupCommitsPath, repoPath } from "@/lib/paths"
import { CommitList } from "@/shared/commit-list"
import { EmptyState } from "@/shared/empty-state"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { PanelBoundary } from "@/shared/panel-boundary"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { ActivityLight, HealthBadges } from "@/shared/health"
import { VisibilityBadge } from "@/shared/visibility-badge"
import { groupCommitsQuery, groupQuery } from "./api"
import { validateGroupSearch, type GroupSearch, type SortKey } from "./search"

/** view filters repositories by name or description and sorts them. */
export function view(repos: Repo[], q = "", sort: SortKey = "name", desc = false): Repo[] {
  const needle = q.trim().toLowerCase()
  const out = repos.filter((r) => !needle || r.address.includes(needle) || r.description.toLowerCase().includes(needle))
  out.sort((a, b) => (sort === "pushed" ? a.pushedAt.localeCompare(b.pushedAt) : a.address.localeCompare(b.address)))
  return desc ? out.reverse() : out
}

/** searchOf writes the view state of the group page as search parameters. */
function searchOf(s: GroupSearch): string {
  const p = new URLSearchParams()
  if (s.q) p.set("q", s.q)
  if (s.sort && s.sort !== "name") p.set("sort", s.sort)
  if (s.desc) p.set("desc", "true")
  const q = p.toString()
  return q ? `?${q}` : ""
}

/** GroupView is a group: its repositories (filtered and sorted through the URL) and its latest commits. */
export function GroupView({ group }: { group: string }) {
  const params = useSearchParams()
  const search = validateGroupSearch(Object.fromEntries(params))
  const { q, sort = "name", desc = false } = search
  const router = useRouter()
  const pathname = usePathname()
  const navigate = (next: GroupSearch) => router.replace(`${pathname}${searchOf(next)}`, { scroll: false })
  const [text, setText] = useState(q ?? "") // the filter as typed; the URL follows it
  const { data, error } = useQuery(groupQuery(api, group))
  const repos = data ? view(data.repos, text, sort, desc) : []

  function header(key: SortKey, label: string) {
    const active = sort === key
    return (
      <th scope="col" aria-sort={active ? (desc ? "descending" : "ascending") : "none"} className="p-2 text-left">
        <button
          type="button"
          className="inline-flex min-h-6 items-center gap-1 font-semibold hover:underline"
          onClick={() => navigate({ ...search, sort: key, desc: active ? !desc || undefined : undefined })}
        >
          {label}
          <span aria-hidden="true">{active ? (desc ? "↓" : "↑") : "↕"}</span>
        </button>
      </th>
    )
  }

  return (
    <>
      <PageHeader title={data?.name ?? group}>
        {data?.description && <p className="text-muted-foreground">{data.description}</p>}
      </PageHeader>
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <div className="grid gap-6 lg:grid-cols-3">
          <section aria-labelledby="repos" className="min-w-0 lg:col-span-2">
            <div className="mb-3 flex flex-wrap items-end justify-between gap-2">
              <h2 id="repos" className="text-lg font-semibold">
                {plural(data.repoCount, "repository", "repositories")}
              </h2>
              <label className="flex flex-col text-sm">
                Filter by name
                <input
                  type="search"
                  value={text}
                  onChange={(e) => {
                    setText(e.target.value)
                    navigate({ ...search, q: e.target.value || undefined })
                  }}
                  className="mt-1 min-h-9 rounded-md border border-input bg-background px-2"
                />
              </label>
            </div>
            {repos.length === 0 ? (
              <EmptyState title={text ? "No repository matches the filter" : "No repositories in this group"} />
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead className="border-b border-border">
                    <tr>
                      {header("name", "Repository")}
                      <th scope="col" className="p-2 text-left font-semibold">
                        Description
                      </th>
                      {header("pushed", "Last push")}
                      <th scope="col" className="p-2 text-left font-semibold">
                        Health
                      </th>
                      <th scope="col" className="p-2 text-left font-semibold">
                        Freshness
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {repos.map((r) => (
                      <tr key={r.address}>
                        <td className="p-2 font-medium">
                          <span className="inline-flex flex-wrap items-center gap-2">
                            <ActivityLight repo={r} decorative />
                            <Link href={repoPath(r.owner, r.name)} className="hover:underline">
                              {r.owner}/{r.name}
                            </Link>
                            <VisibilityBadge visibility={r.visibility} />
                          </span>
                        </td>
                        <td className="p-2 text-muted-foreground">{r.description}</td>
                        <td className="p-2 whitespace-nowrap">
                          <RelativeTime at={r.pushedAt} />
                        </td>
                        <td className="p-2">
                          <HealthBadges repo={r} className="min-w-72" />
                        </td>
                        <td className="p-2">
                          <FreshnessBadge freshness={r.freshness} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
          <PanelBoundary>
            <LatestCommits group={group} />
          </PanelBoundary>
        </div>
      )}
    </>
  )
}

function LatestCommits({ group }: { group: string }) {
  const { data, error } = useQuery(groupCommitsQuery(api, group))
  return (
    <Card aria-labelledby="latest">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="latest">Latest commits</CardTitle>
        {data && <FreshnessBadge freshness={data.freshness} />}
      </div>
      {error && !data && <ProblemAlert error={error} />}
      {data && <CommitList commits={data.commits} />}
      <Link href={groupCommitsPath(group)} className="mt-2 inline-block text-sm hover:underline">
        Commit log of the group
      </Link>
    </Card>
  )
}
