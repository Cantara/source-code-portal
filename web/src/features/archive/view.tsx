"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useRef, useState, type FormEvent } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api/client"
import { archiveQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { crumbs } from "@/lib/crumbs"
import { count, plural } from "@/lib/format"
import { firstPageOf, rememberSize } from "@/lib/paging"
import { repoPath } from "@/lib/paths"
import { filterField, ProductField } from "@/shared/filter-fields"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { PageSizeField, Pager } from "@/shared/pager"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { archiveSearch, archiveSorts, archiveState } from "./api"

const reasons = { archived: "Archived", deprecated: "Deprecated" } as const

/**
 * ArchiveView is the retired repositories of the reader's view (design/archive.md): why each is retired, when it was last active, what
 * still depends on it and how many authors it had. The reasons as chips with their counts; the product, the name, whether it is
 * still used and the order in a GET form that works without JavaScript; numbered pages of the reader's size.
 */
export function ArchiveView({ defaultSize }: { defaultSize: number }) {
  const st = archiveState(Object.fromEntries(useSearchParams()), defaultSize)
  const path = usePathname()
  const router = useRouter()
  const top = useRef<HTMLElement>(null)
  const { data, error, isPlaceholderData } = useQuery(archiveQuery(api, { ...st.filter, page: st.page, size: st.size }))
  const href = (n: number) => path + archiveSearch({ ...st, page: n }, defaultSize)
  const [text, setText] = useState(st.filter.q ?? "")
  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const fields: Record<string, string> = {}
    for (const [k, v] of new FormData(e.currentTarget)) if (typeof v === "string") fields[k] = v
    const next = archiveState({ ...fields, q: text, per: String(st.size) }, defaultSize)
    router.push(`/archive${archiveSearch({ ...next, page: 1 }, defaultSize)}`)
  }
  function goPage(n: number) {
    window.history.pushState(null, "", href(n))
    top.current?.scrollIntoView({ block: "start" })
  }
  return (
    <>
      <PageHeader
        title="Archive"
        trail={crumbs.archive()}
        meta={
          data && (
            <span className="font-mono text-xs text-muted-foreground">
              {plural(data.counts.total, "retired repository", "retired repositories")}
            </span>
          )
        }
      >
        <form
          onSubmit={submit}
          action="/archive"
          className="flex flex-wrap items-end gap-3 text-sm"
          aria-label="Filter the archive"
        >
          {st.filter.reason && <input type="hidden" name="reason" value={st.filter.reason} />}
          <ProductField value={st.filter.group} />
          <label className="flex flex-col">
            Name contains
            <input name="q" value={text} onChange={(e) => setText(e.target.value)} className={filterField} />
          </label>
          <label className="flex flex-col">
            Order
            <select name="sort" defaultValue={st.filter.sort ?? "active"} className={filterField}>
              {archiveSorts.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
          </label>
          <label className="flex min-h-9 items-center gap-2">
            <input type="checkbox" name="used" value="true" defaultChecked={st.filter.used === true} />
            Still depended on
          </label>
          <PageSizeField
            value={st.size}
            onSize={(n) => {
              rememberSize("archive", n)
              window.history.pushState(
                null,
                "",
                path + archiveSearch({ ...st, size: n, page: firstPageOf(st.page, st.size, n) }, defaultSize),
              )
            }}
          />
          <Button type="submit" variant="outline">
            Apply
          </Button>
        </form>
      </PageHeader>
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <section
          ref={top}
          aria-label="Retired repositories"
          aria-busy={isPlaceholderData}
          className="flex scroll-mt-4 flex-col gap-3"
        >
          <nav aria-label="By reason">
            <ul className="flex flex-wrap gap-2 text-sm">
              {([undefined, "archived", "deprecated"] as const).map((r) => (
                <li key={r ?? "all"}>
                  <Link
                    href={path + archiveSearch({ ...st, filter: { ...st.filter, reason: r }, page: 1 }, defaultSize)}
                    aria-current={st.filter.reason === r ? "true" : undefined}
                    className={cn(
                      "inline-flex min-h-9 items-center gap-1.5 rounded-md border px-2.5",
                      st.filter.reason === r ? "border-primary bg-secondary" : "border-border",
                    )}
                  >
                    {r ? reasons[r] : "All"}{" "}
                    <span className="font-mono tabular-nums">{count(r ? data.counts[r] : data.counts.total)}</span>
                  </Link>
                </li>
              ))}
            </ul>
          </nav>
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <FreshnessBadge freshness={data.freshness} />
            {data.counts.used > 0 && <span>{count(data.counts.used)} still depended on by other repositories.</span>}
          </div>
          <Pager page={data.page} href={href} onPage={goPage} noun="retired repositories" />
          <div className={cn("panel px-3", isPlaceholderData && "opacity-60")}>
            {data.archive.length === 0 ? (
              <p className="py-6 text-sm text-muted-foreground">
                {data.counts.total === 0 && !st.filter.q && !st.filter.group
                  ? "No repository has been retired."
                  : "No retired repository matches."}
              </p>
            ) : (
              <ul aria-label="Archive" className="divide-y divide-border">
                {data.archive.map((a) => (
                  <li key={a.repo.address} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 py-3">
                    <Link href={repoPath(a.repo.owner, a.repo.name)} className="font-medium underline-offset-2 hover:underline">
                      {a.repo.address}
                    </Link>
                    <Badge variant={a.reason === "archived" ? "warn" : "default"}>{reasons[a.reason]}</Badge>
                    <span className="text-sm text-muted-foreground">
                      last active <RelativeTime at={a.lastActive} />
                    </span>
                    {a.repo.release && <span className="font-mono text-xs text-muted-foreground">{a.repo.release.tag}</span>}
                    {a.usedBy > 0 && <Badge variant="error">used by {count(a.usedBy)}</Badge>}
                    {a.authors > 0 && (
                      <span className="text-xs text-muted-foreground">{plural(a.authors, "author", "authors")}</span>
                    )}
                    {a.repo.description && <p className="basis-full text-sm text-muted-foreground">{a.repo.description}</p>}
                  </li>
                ))}
              </ul>
            )}
          </div>
          {data.archive.length > 0 && (
            <Pager page={data.page} href={href} onPage={goPage} noun="retired repositories" label="Pages, at the end" />
          )}
          {error && <ProblemAlert error={error} />}
        </section>
      )}
    </>
  )
}
