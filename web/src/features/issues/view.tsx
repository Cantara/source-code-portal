"use client"

import { useQuery } from "@tanstack/react-query"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useRef } from "react"
import { api } from "@/lib/api/client"
import { issuesQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { crumbs } from "@/lib/crumbs"
import { plural } from "@/lib/format"
import { firstPageOf, rememberSize } from "@/lib/paging"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { Pager } from "@/shared/pager"
import { PrerequisiteNotice } from "@/shared/prerequisites"
import { ProblemAlert } from "@/shared/problem-alert"
import { filterField } from "@/shared/filter-fields"
import { QueueChips, QueueForm } from "@/shared/queue"
import { RelativeTime } from "@/shared/relative-time"
import { IssueList, issueKinds, issueStates } from "@/entities/issue/issue"
import { issuesSearch, issuesState } from "./api"

/**
 * IssuesView is every open issue of the reader's view, what waits first (design/issues.md): the states as chips with their counts,
 * which filter; the kind, the product, the repository, whose and the order in a GET form that works without JavaScript; numbered
 * pages of the reader's size (defaultSize until chosen).
 */
export function IssuesView({ defaultSize }: { defaultSize: number }) {
  const st = issuesState(Object.fromEntries(useSearchParams()), defaultSize)
  const path = usePathname()
  const router = useRouter()
  const top = useRef<HTMLElement>(null)
  const { data, error, isPlaceholderData } = useQuery(issuesQuery(api, { ...st.filter, page: st.page, size: st.size }))
  const href = (n: number) => path + issuesSearch({ ...st, page: n }, defaultSize)
  function goPage(n: number) {
    window.history.pushState(null, "", href(n))
    top.current?.scrollIntoView({ block: "start" })
  }
  return (
    <>
      <PageHeader
        title="Issues"
        trail={crumbs.issues()}
        meta={data && <span className="font-mono text-xs text-muted-foreground">{plural(data.counts.open, "open", "open")}</span>}
      >
        <QueueForm
          key={issuesSearch({ ...st, page: 1 }, defaultSize)}
          action="/issues"
          label="Filter issues"
          filter={st.filter}
          waiting="What waits, longest first"
          before={
            <label className="flex flex-col">
              Kind
              <select name="kind" defaultValue={st.filter.kind ?? ""} className={filterField}>
                <option value="">Every kind</option>
                {issueKinds.map((k) => (
                  <option key={k.kind} value={k.kind}>
                    {k.label}
                  </option>
                ))}
              </select>
            </label>
          }
          size={st.size}
          onSize={(n) => {
            rememberSize("issues", n)
            window.history.pushState(
              null,
              "",
              path + issuesSearch({ ...st, size: n, page: firstPageOf(st.page, st.size, n) }, defaultSize),
            )
          }}
          onApply={(fields) => {
            const next = issuesState({ ...fields, state: st.filter.state, per: String(st.size) }, defaultSize)
            router.push(`/issues${issuesSearch({ ...next, page: 1 }, defaultSize)}`)
          }}
        />
      </PageHeader>
      <PrerequisiteNotice feature="issues" />
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <section ref={top} aria-label="Open issues" aria-busy={isPlaceholderData} className="flex scroll-mt-4 flex-col gap-3">
          <QueueChips
            label="By where they stand"
            states={issueStates}
            open={data.counts.open}
            counts={data.counts.states}
            current={st.filter.state}
            to={(state) => path + issuesSearch({ ...st, filter: { ...st.filter, state }, page: 1 }, defaultSize)}
          />
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <FreshnessBadge freshness={data.freshness} />
            {data.counts.oldestTriage && (
              <span>
                The person&apos;s issue waiting longest for triage was opened <RelativeTime at={data.counts.oldestTriage} />.
              </span>
            )}
          </div>
          <Pager page={data.page} href={href} onPage={goPage} noun="issues" />
          <div className={cn("panel px-3", isPlaceholderData && "opacity-60")}>
            <IssueList
              issues={data.issues}
              empty={st.filter.state || st.filter.kind ? "No open issue matches" : "No open issues"}
            />
          </div>
          {data.issues.length > 0 && (
            <Pager page={data.page} href={href} onPage={goPage} noun="issues" label="Pages, at the end" />
          )}
          {error && <ProblemAlert error={error} />}
        </section>
      )}
    </>
  )
}
