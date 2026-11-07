"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { usePathname, useSearchParams } from "next/navigation"
import { Card, CardTitle } from "@/components/ui/card"
import { api, type Repo, type Schemas } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { count, plural } from "@/lib/format"
import { groupCommitsPath, repoPath } from "@/lib/paths"
import { CommitList } from "@/entities/commit/commit-list"
import { EmptyState } from "@/shared/empty-state"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { crumbs } from "@/lib/crumbs"
import { PageHeader } from "@/shared/page-header"
import { PanelBoundary } from "@/shared/panel-boundary"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { ActivityLight } from "@/entities/repo/activity-light"
import { lastChange } from "@/entities/repo/health"
import { licence } from "@/entities/repo/licence"
import { ecosystemLabel, newestPackage } from "@/entities/repo/packages"
import { levels } from "@/entities/health/health"
import { Hint } from "@/shared/hint"
import { cn } from "@/lib/cn"
import { FindingList, HealthScore } from "@/entities/health/findings"
import { VisibilityBadge } from "@/entities/repo/visibility-badge"
import { archiveQuery, groupQuery, issuesQuery, pullsQuery, releasesQuery } from "@/lib/api/queries"
import { useFeatures } from "@/lib/rules-context"
import { PullList } from "@/entities/pull/pull"
import { ReleaseList } from "@/entities/release/release"
import { IssueList } from "@/entities/issue/issue"
import { groupCommitsQuery, groupIssues, groupPulls, groupReleases, groupRetired } from "./api"
import { validateGroupSearch, type GroupSearch, type SortKey } from "./search"

/** Column is one column of the repository table: its header, how it sorts and how a row draws it. */
type Column = {
  key: SortKey
  label: string
  numeric?: boolean
  /** a count read on its own: centred under its header */
  center?: boolean
  value: (r: Repo) => string | number
}

/** from is where a repository's release is published: GitHub for a GitHub release, else its newest package's registry. */
const from = (r: Repo) =>
  r.release ? "GitHub" : newestPackage(r) ? ecosystemLabel(newestPackage(r)?.ecosystem ?? "") : undefined

const released = (r: Repo) => r.release?.publishedAt ?? newestPackage(r)?.publishedAt ?? ""

/** columns are the table's columns, as the dashboard's product table draws its own: numbers right, every one sortable. */
const columns: Column[] = [
  { key: "name", label: "Repository", value: (r) => r.name.toLowerCase() },
  { key: "release", label: "Release", value: released },
  { key: "from", label: "From", value: (r) => from(r) ?? "" },
  { key: "licence", label: "Licence", value: (r) => r.license },
  { key: "issues", label: "Issues", numeric: true, center: true, value: (r) => r.openIssues },
  { key: "pulls", label: "PRs", numeric: true, center: true, value: (r) => r.openPulls ?? -1 },
  { key: "last", label: "Last change", value: (r) => lastChange(r) },
  { key: "findings", label: "Findings", numeric: true, center: true, value: (r) => r.health?.findings.length ?? -1 },
  { key: "health", label: "Health", numeric: true, value: (r) => r.health?.score ?? -1 },
]

/** view filters repositories by name or description and sorts them by a column. */
export function view(repos: Repo[], q = "", sort: SortKey = "name", desc = false): Repo[] {
  const needle = q.trim().toLowerCase()
  const out = repos.filter((r) => !needle || r.address.includes(needle) || r.description.toLowerCase().includes(needle))
  const col = columns.find((c) => c.key === sort)
  out.sort((a, b) => {
    const x = col?.value(a) ?? ""
    const y = col?.value(b) ?? ""
    return x < y ? -1 : x > y ? 1 : a.address.localeCompare(b.address)
  })
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

const none = <span className="text-muted-foreground">–</span>

/** Release is a repository's latest GitHub release, else its newest published package, as a link. */
function Release({ r }: { r: Repo }) {
  const pkg = newestPackage(r)
  if (r.release)
    return (
      <a href={r.release.url} rel="noreferrer nofollow" className="block max-w-36 truncate hover:underline" title={r.release.tag}>
        <span className="sr-only">Release </span>
        {r.release.tag}
      </a>
    )
  if (!pkg) return <span className="text-muted-foreground">none</span>
  return pkg.url ? (
    <a
      href={pkg.url}
      rel="noreferrer nofollow"
      aria-label={`${pkg.version}, published package ${pkg.name} on ${ecosystemLabel(pkg.ecosystem)}`}
      title={pkg.name}
      className="block max-w-36 truncate hover:underline"
    >
      {pkg.version}
    </a>
  ) : (
    <span className="block max-w-36 truncate" title={pkg.name}>
      {pkg.version}
    </span>
  )
}

/** Findings is how many findings a repository has, with what each costs in a hint, linking to them on its page. */
function Findings({ r }: { r: Repo }) {
  const health = r.health
  if (!health) return none
  const fs = health.findings
  if (fs.length === 0) return <span className="text-muted-foreground">0</span>
  return (
    <Hint
      heading={plural(fs.length, "finding", "findings")}
      swatch={levels[health.level].className}
      rows={fs.map((f) => ({ swatch: levels[health.level].className, label: f.message, value: `−${f.penalty}` }))}
      render={(t) => (
        <Link {...t} href={`${repoPath(r.owner, r.name)}#health`} className="hover:text-primary hover:underline">
          {count(fs.length)}
          <span className="sr-only"> {fs.length === 1 ? "finding" : "findings"}</span>
        </Link>
      )}
    />
  )
}

/** GroupView is a group: its repositories (filtered and sorted through the URL) and its latest commits. */
export function GroupView({ group }: { group: string }) {
  const params = useSearchParams()
  const search = validateGroupSearch(Object.fromEntries(params))
  const { q, sort = "name", desc = false } = search
  const pathname = usePathname()
  // The view state lives in the URL, written in place: Next.js follows history.replaceState without asking the server again.
  const navigate = (next: GroupSearch) => window.history.replaceState(null, "", `${pathname}${searchOf(next)}`)
  const text = q ?? ""
  const now = useNow()
  const { data, error } = useQuery(groupQuery(api, group))
  const features = useFeatures()
  const shown = columns.filter((c) => c.key !== "pulls" || features.pulls)
  const repos = data ? view(data.repos, text, sort, desc) : []
  // One owner (the usual case) goes unsaid on every row; several are named.
  const owners = new Set(data?.repos.map((r) => r.owner) ?? [])

  const header = (c: Column) => {
    const active = sort === c.key
    return (
      <th
        key={c.key}
        scope="col"
        aria-sort={active ? (desc ? "descending" : "ascending") : undefined}
        className={cn(
          "px-3 py-2 font-medium whitespace-nowrap",
          c.center ? "text-center" : c.numeric ? "text-right" : "text-left",
        )}
      >
        <button
          type="button"
          className="inline-flex items-center gap-1 hover:text-foreground"
          onClick={() => navigate({ ...search, sort: c.key, desc: (active ? !desc : !!c.numeric) || undefined })}
        >
          {c.label}
          <span aria-hidden="true">{active ? (desc ? "▼" : "▲") : ""}</span>
        </button>
      </th>
    )
  }

  return (
    <>
      <PageHeader title={data?.name ?? group} trail={crumbs.group(data?.id ?? group, data?.name ?? group)}>
        {data?.description && <p className="text-muted-foreground">{data.description}</p>}
      </PageHeader>
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
          <section aria-labelledby="repos" className="min-w-0">
            <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
              <h2 id="repos" className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
                {plural(data.repoCount, "repository", "repositories")}
              </h2>
              <input
                type="search"
                aria-label="Filter by name"
                placeholder="Filter by name or description"
                value={text}
                onChange={(e) => navigate({ ...search, q: e.target.value || undefined })}
                className="min-h-8 w-full rounded-md border border-input bg-background px-2.5 text-sm sm:w-64"
              />
            </div>
            {repos.length === 0 ? (
              <EmptyState title={text ? "No repository matches the filter" : "No repositories in this group"} />
            ) : (
              <div className="panel overflow-x-auto">
                <table className="w-full text-sm">
                  <caption className="sr-only">Repositories of {data.name}, sortable by every column</caption>
                  <thead>
                    <tr className="border-b border-border text-xs text-muted-foreground">{shown.map(header)}</tr>
                  </thead>
                  <tbody>
                    {repos.map((r) => {
                      const lic = licence(r.license)
                      return (
                        <tr key={r.address} className="border-b border-border/60 align-middle last:border-b-0">
                          <th scope="row" className="px-3 py-2 text-left font-normal">
                            <div className="flex max-w-[20rem] min-w-44 flex-col">
                              <span className="flex min-w-0 items-center gap-2">
                                <ActivityLight repo={r} now={now} />
                                <Link
                                  href={repoPath(r.owner, r.name)}
                                  className="truncate font-medium hover:text-primary hover:underline"
                                  title={`${r.owner}/${r.name}`}
                                >
                                  {owners.size > 1 && <span className="text-muted-foreground">{r.owner}/</span>}
                                  {r.name}
                                </Link>
                                <VisibilityBadge visibility={r.visibility} />
                                {r.freshness.status !== "fresh" && <FreshnessBadge freshness={r.freshness} />}
                              </span>
                              {(r.language || r.description) && (
                                <span className="truncate pl-4 text-xs text-muted-foreground" title={r.description || undefined}>
                                  {r.language && <span className="font-medium">{r.language}</span>}
                                  {r.language && r.description && " · "}
                                  {r.description}
                                </span>
                              )}
                            </div>
                          </th>
                          <td className="px-3 py-2 font-mono text-xs whitespace-nowrap tabular-nums">
                            <Release r={r} />
                          </td>
                          <td className="px-3 py-2 whitespace-nowrap text-muted-foreground">{from(r) ?? none}</td>
                          <td className="px-3 py-2 font-mono text-xs whitespace-nowrap">
                            {lic ? (
                              <a href={lic.url} rel="noreferrer nofollow" title={lic.spdx} className="hover:underline">
                                {lic.short}
                              </a>
                            ) : (
                              <span className="text-muted-foreground">no licence</span>
                            )}
                          </td>
                          <td className="px-3 py-2 text-center font-mono tabular-nums">
                            {features.issues ? (
                              <Link href={`/issues?repo=${encodeURIComponent(r.address)}`} className="hover:underline">
                                {count(r.openIssues)}
                                <span className="sr-only"> open issues</span>
                              </Link>
                            ) : (
                              <span
                                title={
                                  r.openIssuesWithPulls
                                    ? "Open issues and pull requests together, as GitHub counts them"
                                    : undefined
                                }
                              >
                                {count(r.openIssues)}
                                <span className="sr-only">
                                  {r.openIssuesWithPulls ? " open issues and pull requests" : " open issues"}
                                </span>
                              </span>
                            )}
                          </td>
                          {features.pulls && (
                            <td className="px-3 py-2 text-center font-mono tabular-nums">
                              {r.openPulls === null ? (
                                none
                              ) : (
                                <Link href={`/pulls?repo=${encodeURIComponent(r.address)}`} className="hover:underline">
                                  {count(r.openPulls)}
                                  <span className="sr-only"> open pull requests</span>
                                </Link>
                              )}
                            </td>
                          )}
                          <td className="px-3 py-2 font-mono whitespace-nowrap">
                            <RelativeTime at={lastChange(r)} />
                          </td>
                          <td className="px-3 py-2 text-center font-mono tabular-nums">
                            <Findings r={r} />
                          </td>
                          <td className="px-3 py-2 text-right">
                            {r.health ? <HealthScore health={r.health} className="justify-end" /> : none}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>
          <div className="flex min-w-0 flex-col gap-6">
            {data.health && <GroupHealth health={data.health} />}
            {features.pulls && (
              <PanelBoundary>
                <GroupPulls group={group} />
              </PanelBoundary>
            )}
            {features.issues && (
              <PanelBoundary>
                <GroupIssues group={group} />
              </PanelBoundary>
            )}
            <PanelBoundary>
              <GroupReleases group={group} />
            </PanelBoundary>
            <PanelBoundary>
              <LatestCommits group={group} />
            </PanelBoundary>
            <p className="text-sm">
              <Link href={`/insights?group=${encodeURIComponent(group)}`} className="hover:underline">
                Insights of this product
              </Link>
            </p>
          </div>
        </div>
      )}
    </>
  )
}

/** GroupHealth is the group's weighed score and every finding behind it, the most costly first. */
function GroupHealth({ health }: { health: Schemas["ProductHealth"] }) {
  return (
    <Card aria-labelledby="health">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="health">Health</CardTitle>
        <HealthScore health={health} large />
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        {plural(health.counted, "repository", "repositories")} weighed: {health.attention} at attention, {health.watch} to watch
      </p>
      {health.findings.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">Nothing needs attention.</p>
      ) : (
        <FindingList findings={health.findings} className="mt-2" />
      )}
    </Card>
  )
}

/** GroupPulls is the group's open pull requests whose turn it is first, and the way to all of them on /pulls; none: nothing. */
function GroupPulls({ group }: { group: string }) {
  const { data, error } = useQuery(pullsQuery(api, groupPulls(group)))
  if (data && data.counts.open === 0) return null
  return (
    <Card aria-labelledby="pulls">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="pulls">Pull requests</CardTitle>
        {data && <span className="text-xs text-muted-foreground">{count(data.counts.states.review)} waiting for a review</span>}
      </div>
      {error && !data && <ProblemAlert error={error} />}
      {data && <PullList pulls={data.pulls} compact />}
      <Link href={`/pulls?group=${encodeURIComponent(group)}`} className="mt-2 inline-block text-sm hover:underline">
        {data ? `All ${count(data.counts.open)} open` : "All open pull requests"}
      </Link>
    </Card>
  )
}

/** GroupIssues is the group's open issues that wait longest, and the way to all of them on /issues; none: nothing. */
function GroupIssues({ group }: { group: string }) {
  const { data, error } = useQuery(issuesQuery(api, groupIssues(group)))
  if (data && data.counts.open === 0) return null
  return (
    <Card aria-labelledby="issues">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="issues">Issues</CardTitle>
        {data && <span className="text-xs text-muted-foreground">{count(data.counts.states.triage)} needing triage</span>}
      </div>
      {error && !data && <ProblemAlert error={error} />}
      {data && <IssueList issues={data.issues} compact />}
      <Link href={`/issues?group=${encodeURIComponent(group)}`} className="mt-2 inline-block text-sm hover:underline">
        {data ? `All ${count(data.counts.open)} open` : "All open issues"}
      </Link>
    </Card>
  )
}

/** GroupReleases is the group's latest release of each repository, the newest five, the way to all of them on /releases and, when
 * some of its repositories are retired, the way to them on /archive; a group with neither: nothing. */
function GroupReleases({ group }: { group: string }) {
  const { data, error } = useQuery(releasesQuery(api, groupReleases(group)))
  const retired = useQuery(archiveQuery(api, groupRetired(group))).data?.counts.total ?? 0
  if (data && data.counts.all === 0 && retired === 0) return null
  return (
    <Card aria-labelledby="releases">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <CardTitle id="releases">Releases</CardTitle>
        {data && <FreshnessBadge freshness={data.freshness} />}
      </div>
      {error && !data && <ProblemAlert error={error} />}
      {data && <ReleaseList releases={data.releases} compact empty="None published" />}
      <div className="mt-2 flex flex-wrap gap-x-4 text-sm">
        <Link href={`/releases?group=${encodeURIComponent(group)}`} className="hover:underline">
          All releases
        </Link>
        {retired > 0 && (
          <Link href={`/archive?group=${encodeURIComponent(group)}`} className="text-muted-foreground hover:underline">
            {count(retired)} retired
          </Link>
        )}
      </div>
    </Card>
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
