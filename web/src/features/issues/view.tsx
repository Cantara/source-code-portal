"use client"

import { useQuery } from "@tanstack/react-query"
import { useRef } from "react"
import { api } from "@/lib/api/client"
import { issuesQuery } from "@/lib/api/queries"
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
  useListNav,
} from "@/shared/filters"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PageHeader } from "@/shared/page-header"
import { Pager } from "@/shared/pager"
import { PrerequisiteNotice } from "@/shared/prerequisites"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { IssueList, issueKinds, issueStates } from "@/entities/issue/issue"
import { issuesSearch, issuesState } from "./api"

/**
 * IssuesView is every open issue of the reader's view, what waits first (design/issues.md): the filters in the panel at the left,
 * every choice in view with the states' counts (the kind, the product, the repository, whose, the order, the page size), and
 * numbered pages of the reader's size (defaultSize until chosen).
 */
export function IssuesView({ defaultSize, filters }: { defaultSize: number; filters: Filters }) {
  const { st, href, pick, pickSize, clear } = useListNav({
    list: "issues",
    base: "/issues",
    parse: issuesState,
    serialize: issuesSearch,
    defaultSize,
  })
  const top = useRef<HTMLElement>(null)
  const { data, error, isPlaceholderData } = useQuery(issuesQuery(api, { ...st.filter, page: st.page, size: st.size }))
  function goPage(n: number) {
    window.history.pushState(null, "", href(n))
    top.current?.scrollIntoView({ block: "start" })
  }
  const f = st.filter
  return (
    <>
      <PageHeader
        title="Issues"
        trail={crumbs.issues()}
        meta={data && <span className="font-mono text-xs text-muted-foreground">{plural(data.counts.open, "open", "open")}</span>}
      />
      <PrerequisiteNotice feature="issues" />
      <FilterLayout
        initial={filters}
        label="Filter issues"
        clear={clear}
        filters={
          <FilterForm action="/issues" label="Filter issues">
            <FilterGroup title="Where it stands">
              <Choices
                name="state"
                label="Where it stands"
                value={f.state ?? ""}
                onPick={(state) => pick({ state })}
                options={[
                  { value: "", label: "All open", count: data?.counts.open },
                  ...issueStates.map((s) => ({
                    value: s.state,
                    label: s.label,
                    about: s.waits,
                    dot: s.dot,
                    count: data ? data.counts.states[s.state] : undefined,
                  })),
                ]}
              />
            </FilterGroup>
            <FilterGroup title="Kind">
              <Choices
                name="kind"
                label="Kind"
                value={f.kind ?? ""}
                onPick={(kind) => pick({ kind })}
                options={[{ value: "", label: "Every kind" }, ...issueKinds.map((k) => ({ value: k.kind, label: k.label }))]}
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
                  { value: "waiting", label: "What waits, longest first" },
                  { value: "newest", label: "Newest first" },
                  { value: "updated", label: "Updated last first" },
                ]}
              />
            </FilterGroup>
            <SizeChoices value={st.size} onPick={pickSize} />
          </FilterForm>
        }
      >
        {error && !data && <ProblemAlert error={error} />}
        {data && (
          <section ref={top} aria-label="Open issues" aria-busy={isPlaceholderData} className="flex scroll-mt-4 flex-col gap-3">
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
              <IssueList issues={data.issues} empty={f.state || f.kind ? "No open issue matches" : "No open issues"} />
            </div>
            {data.issues.length > 0 && (
              <Pager page={data.page} href={href} onPage={goPage} noun="issues" label="Pages, at the end" />
            )}
            {error && <ProblemAlert error={error} />}
          </section>
        )}
      </FilterLayout>
    </>
  )
}
