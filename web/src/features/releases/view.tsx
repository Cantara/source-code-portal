"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useRef } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api/client"
import { releasesQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { crumbs } from "@/lib/crumbs"
import { count, plural } from "@/lib/format"
import { firstPageOf, rememberSize } from "@/lib/paging"
import { addressPath } from "@/lib/paths"
import { filterField, ProductField } from "@/shared/filter-fields"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { PageSizeField, Pager } from "@/shared/pager"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { releaseSorts, releasesSearch, releasesState, releaseVariants, releaseWindows } from "./api"

/**
 * ReleasesView is the recent releases of the reader's view (design/releases.md): the variants as chips with their counts (the
 * latest of each repository, every release, stable, pre-releases); the product, the window and the order in a GET form that works
 * without JavaScript; numbered pages of the reader's size (defaultSize until chosen).
 */
export function ReleasesView({ defaultSize }: { defaultSize: number }) {
  const st = releasesState(Object.fromEntries(useSearchParams()), defaultSize)
  const path = usePathname()
  const router = useRouter()
  const top = useRef<HTMLElement>(null)
  const { data, error, isPlaceholderData } = useQuery(releasesQuery(api, { ...st.filter, page: st.page, size: st.size }))
  const variant = st.filter.variant ?? "latest"
  const href = (n: number) => path + releasesSearch({ ...st, page: n }, defaultSize)
  function goPage(n: number) {
    window.history.pushState(null, "", href(n))
    top.current?.scrollIntoView({ block: "start" })
  }
  return (
    <>
      <PageHeader
        title="Releases"
        trail={crumbs.releases()}
        meta={
          data && (
            <span className="font-mono text-xs text-muted-foreground">{plural(data.counts.all, "release", "releases")} kept</span>
          )
        }
      >
        <form
          action="/releases"
          className="flex flex-wrap items-end gap-3 text-sm"
          aria-label="Filter the releases"
          onSubmit={(e) => {
            e.preventDefault()
            const fields: Record<string, string> = {}
            for (const [k, v] of new FormData(e.currentTarget)) if (typeof v === "string") fields[k] = v
            const next = releasesState({ ...fields, variant: st.filter.variant, per: String(st.size) }, defaultSize)
            router.push(`/releases${releasesSearch({ ...next, page: 1 }, defaultSize)}`)
          }}
        >
          {st.filter.variant && <input type="hidden" name="variant" value={st.filter.variant} />}
          {st.filter.repo && <input type="hidden" name="repo" value={st.filter.repo} />}
          <ProductField value={st.filter.group} />
          <label className="flex flex-col">
            Published
            <select name="since" defaultValue={st.filter.since ?? ""} className={filterField}>
              {releaseWindows.map((w) => (
                <option key={w.value} value={w.value}>
                  {w.label}
                </option>
              ))}
              {st.filter.since && !releaseWindows.some((w) => w.value === st.filter.since) && (
                <option value={st.filter.since}>Since {st.filter.since}</option>
              )}
            </select>
          </label>
          <label className="flex flex-col">
            Order
            <select name="sort" defaultValue={st.filter.sort ?? "newest"} className={filterField}>
              {releaseSorts.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
          </label>
          <PageSizeField
            value={st.size}
            onSize={(n) => {
              rememberSize("releases", n)
              window.history.pushState(
                null,
                "",
                path + releasesSearch({ ...st, size: n, page: firstPageOf(st.page, st.size, n) }, defaultSize),
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
        <section ref={top} aria-label="Releases" aria-busy={isPlaceholderData} className="flex scroll-mt-4 flex-col gap-3">
          <nav aria-label="By variant">
            <ul className="flex flex-wrap gap-2 text-sm">
              {releaseVariants.map((v) => (
                <li key={v.value}>
                  <Link
                    href={path + releasesSearch({ ...st, filter: { ...st.filter, variant: v.value }, page: 1 }, defaultSize)}
                    aria-current={variant === v.value ? "true" : undefined}
                    className={cn(
                      "inline-flex min-h-9 items-center gap-1.5 rounded-md border px-2.5",
                      variant === v.value ? "border-primary bg-secondary" : "border-border",
                    )}
                  >
                    {v.label} <span className="font-mono tabular-nums">{count(data.counts[v.value])}</span>
                  </Link>
                </li>
              ))}
            </ul>
          </nav>
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <FreshnessBadge freshness={data.freshness} />
          </div>
          <Pager page={data.page} href={href} onPage={goPage} noun="releases" />
          <div className={cn("panel px-3", isPlaceholderData && "opacity-60")}>
            {data.releases.length === 0 ? (
              <p className="py-6 text-sm text-muted-foreground">
                {data.counts.all === 0 && !st.filter.since ? "No release has been published." : "No release matches."}
              </p>
            ) : (
              <ul aria-label="Release list" className="divide-y divide-border">
                {data.releases.map((r) => (
                  <li key={`${r.repo}@${r.tag}`} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 py-3">
                    <Link href={addressPath(r.repo)} className="font-medium underline-offset-2 hover:underline">
                      {r.repo}
                    </Link>
                    <a href={r.url} className="font-mono text-sm underline-offset-2 hover:underline">
                      {r.tag}
                    </a>
                    {r.prerelease && <Badge variant="warn">Pre-release</Badge>}
                    {r.title && r.title !== r.tag && <span className="text-sm">{r.title}</span>}
                    <span className="text-sm text-muted-foreground">
                      <RelativeTime at={r.publishedAt} />
                      {r.author && <> by {r.author}</>}
                      {r.bot && <> (automation)</>}
                    </span>
                    {r.summary && <p className="basis-full text-sm text-muted-foreground">{r.summary}</p>}
                  </li>
                ))}
              </ul>
            )}
          </div>
          {data.releases.length > 0 && (
            <Pager page={data.page} href={href} onPage={goPage} noun="releases" label="Pages, at the end" />
          )}
          {error && <ProblemAlert error={error} />}
        </section>
      )}
    </>
  )
}
