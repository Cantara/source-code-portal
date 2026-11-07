"use client"

import { useQuery } from "@tanstack/react-query"
import { useRef } from "react"
import { api } from "@/lib/api/client"
import { pullsQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { crumbs } from "@/lib/crumbs"
import { plural } from "@/lib/format"
import type { Filters } from "@/lib/preferences"
import {
  Choices,
  FilterForm,
  FilterGroup,
  FilterLayout,
  ProductChoices,
  SizeChoices,
  TextFilter,
  Toggle,
  useListNav,
} from "@/shared/filters"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { Pager } from "@/shared/pager"
import { PrerequisiteNotice } from "@/shared/prerequisites"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { PullList, pullStates } from "@/entities/pull/pull"
import { pullsSearch, pullsState } from "./api"

/**
 * PullsView is every open pull request of the reader's view, whose turn first (design/pulls.md): the filters in the panel at the
 * left, every choice in view with the states' counts (the product, the repository, whose, drafts, the order, the page size), and
 * numbered pages of the reader's size (defaultSize until chosen).
 */
export function PullsView({ defaultSize, filters }: { defaultSize: number; filters: Filters }) {
  const { st, href, pick, pickSize, clear } = useListNav({
    list: "pulls",
    base: "/pulls",
    parse: pullsState,
    serialize: pullsSearch,
    defaultSize,
  })
  const top = useRef<HTMLElement>(null)
  const { data, error, isPlaceholderData } = useQuery(pullsQuery(api, { ...st.filter, page: st.page, size: st.size }))
  function goPage(n: number) {
    window.history.pushState(null, "", href(n))
    top.current?.scrollIntoView({ block: "start" })
  }
  const f = st.filter
  return (
    <>
      <PageHeader
        title="Pull requests"
        trail={crumbs.pulls()}
        meta={data && <span className="font-mono text-xs text-muted-foreground">{plural(data.counts.open, "open", "open")}</span>}
      />
      <PrerequisiteNotice feature="pulls" />
      <FilterLayout
        initial={filters}
        label="Filter pull requests"
        clear={clear}
        filters={
          <FilterForm action="/pulls" label="Filter pull requests">
            <FilterGroup title="Whose turn">
              <Choices
                name="state"
                label="Whose turn"
                value={f.state ?? ""}
                onPick={(state) => pick({ state })}
                options={[
                  { value: "", label: "All open", count: data?.counts.open },
                  ...pullStates.map((s) => ({
                    value: s.state,
                    label: s.label,
                    about: s.waits,
                    dot: s.dot,
                    count: data ? data.counts.states[s.state] : undefined,
                  })),
                ]}
              />
            </FilterGroup>
            <ProductChoices value={f.group ?? ""} onPick={(group) => pick({ group })} />
            <FilterGroup title="Repository">
              <TextFilter name="repo" label="Repository (owner/name)" value={f.repo ?? ""} onPick={(repo) => pick({ repo })} />
            </FilterGroup>
            <FilterGroup title="Opened by">
              <Choices
                name="by"
                label="Opened by"
                value={f.by ?? ""}
                onPick={(by) => pick({ by })}
                options={[
                  { value: "", label: "People and automation" },
                  { value: "people", label: "People" },
                  { value: "automation", label: "Automation" },
                ]}
              />
            </FilterGroup>
            <FilterGroup title="Order">
              <Choices
                name="sort"
                label="Order"
                value={f.sort ?? "waiting"}
                onPick={(sort) => pick({ sort })}
                options={[
                  { value: "waiting", label: "Whose turn, waiting longest first" },
                  { value: "newest", label: "Newest first" },
                  { value: "updated", label: "Updated last first" },
                ]}
              />
            </FilterGroup>
            <FilterGroup title="Show">
              <Toggle
                name="drafts"
                label="Drafts"
                checked={f.drafts === true}
                onPick={(on) => pick({ drafts: on ? "true" : "" })}
              />
            </FilterGroup>
            <SizeChoices value={st.size} onPick={pickSize} />
          </FilterForm>
        }
      >
        {error && !data && <ProblemAlert error={error} />}
        {data && (
          <section
            ref={top}
            aria-label="Open pull requests"
            aria-busy={isPlaceholderData}
            className="flex scroll-mt-4 flex-col gap-3"
          >
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
              <PullList pulls={data.pulls} empty={f.state ? "No open pull request in this state" : "No open pull requests"} />
            </div>
            {data.pulls.length > 0 && (
              <Pager page={data.page} href={href} onPage={goPage} noun="pull requests" label="Pages, at the end" />
            )}
            {error && <ProblemAlert error={error} />}
          </section>
        )}
      </FilterLayout>
    </>
  )
}
