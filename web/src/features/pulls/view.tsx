"use client"

import { useQuery } from "@tanstack/react-query"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useRef } from "react"
import { api } from "@/lib/api/client"
import { pullsQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { crumbs } from "@/lib/crumbs"
import { plural } from "@/lib/format"
import { firstPageOf, rememberSize } from "@/lib/paging"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { Pager } from "@/shared/pager"
import { ProblemAlert } from "@/shared/problem-alert"
import { QueueChips, QueueForm } from "@/shared/queue"
import { RelativeTime } from "@/shared/relative-time"
import { PullList, pullStates } from "@/entities/pull/pull"
import { pullsSearch, pullsState } from "./api"

/**
 * PullsView is every open pull request of the reader's view, whose turn first (design/pulls.md): the states as chips with their
 * counts, which filter; the product, the repository, whose, drafts and the order in a GET form that works without JavaScript; numbered
 * pages of the reader's size (defaultSize until chosen).
 */
export function PullsView({ defaultSize }: { defaultSize: number }) {
  const st = pullsState(Object.fromEntries(useSearchParams()), defaultSize)
  const path = usePathname()
  const router = useRouter()
  const top = useRef<HTMLElement>(null)
  const { data, error, isPlaceholderData } = useQuery(pullsQuery(api, { ...st.filter, page: st.page, size: st.size }))
  const href = (n: number) => path + pullsSearch({ ...st, page: n }, defaultSize)
  function goPage(n: number) {
    window.history.pushState(null, "", href(n))
    top.current?.scrollIntoView({ block: "start" })
  }
  return (
    <>
      <PageHeader
        title="Pull requests"
        trail={crumbs.pulls()}
        meta={data && <span className="font-mono text-xs text-muted-foreground">{plural(data.counts.open, "open", "open")}</span>}
      >
        <QueueForm
          key={pullsSearch({ ...st, page: 1 }, defaultSize)}
          action="/pulls"
          label="Filter pull requests"
          filter={st.filter}
          waiting="Whose turn, waiting longest first"
          after={
            <label className="flex min-h-9 items-center gap-2">
              <input type="checkbox" name="drafts" value="true" defaultChecked={st.filter.drafts === true} />
              Show drafts
            </label>
          }
          size={st.size}
          onSize={(n) => {
            rememberSize("pulls", n)
            window.history.pushState(
              null,
              "",
              path + pullsSearch({ ...st, size: n, page: firstPageOf(st.page, st.size, n) }, defaultSize),
            )
          }}
          onApply={(fields) => {
            const next = pullsState({ ...fields, state: st.filter.state, per: String(st.size) }, defaultSize)
            router.push(`/pulls${pullsSearch({ ...next, page: 1 }, defaultSize)}`)
          }}
        />
      </PageHeader>
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <section
          ref={top}
          aria-label="Open pull requests"
          aria-busy={isPlaceholderData}
          className="flex scroll-mt-4 flex-col gap-3"
        >
          <QueueChips
            label="By whose turn"
            states={pullStates}
            open={data.counts.open}
            counts={data.counts.states}
            current={st.filter.state}
            to={(state) => path + pullsSearch({ ...st, filter: { ...st.filter, state }, page: 1 }, defaultSize)}
          />
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <FreshnessBadge freshness={data.freshness} />
            {data.counts.oldestReview && (
              <span>
                The person&apos;s pull request waiting longest for a review was last updated{" "}
                <RelativeTime at={data.counts.oldestReview} />.
              </span>
            )}
          </div>
          <Pager page={data.page} href={href} onPage={goPage} noun="pull requests" />
          <div className={cn("panel px-3", isPlaceholderData && "opacity-60")}>
            <PullList
              pulls={data.pulls}
              empty={st.filter.state ? "No open pull request in this state" : "No open pull requests"}
            />
          </div>
          {data.pulls.length > 0 && (
            <Pager page={data.page} href={href} onPage={goPage} noun="pull requests" label="Pages, at the end" />
          )}
          {error && <ProblemAlert error={error} />}
        </section>
      )}
    </>
  )
}
