"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useRef } from "react"
import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api/client"
import { archiveQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { crumbs } from "@/lib/crumbs"
import { count, plural } from "@/lib/format"
import { repoPath } from "@/lib/paths"
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
import { EmptyState } from "@/shared/empty-state"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { InitialsAvatar } from "@/shared/initials-avatar"
import { ListRow } from "@/shared/list-row"
import { PageHeader } from "@/shared/page-header"
import { Pager } from "@/shared/pager"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { archiveSearch, archiveSorts, archiveState } from "./api"

const reasons = { archived: "Archived", deprecated: "Deprecated" } as const

/**
 * ArchiveView is the retired repositories of the reader's view (design/archive.md): why each is retired, when it was last active, what
 * still depends on it and how many authors it had. The filters are in the panel at the left, every choice in view with the reasons'
 * counts (the product, the name, whether it is still used, the order, the page size); numbered pages of the reader's size.
 */
export function ArchiveView({ defaultSize, filters }: { defaultSize: number; filters: Filters }) {
  const { st, href, pick, pickSize, clear } = useListNav({
    list: "archive",
    base: "/archive",
    parse: archiveState,
    serialize: archiveSearch,
    defaultSize,
  })
  const top = useRef<HTMLElement>(null)
  const { data, error, isPlaceholderData } = useQuery(archiveQuery(api, { ...st.filter, page: st.page, size: st.size }))
  function goPage(n: number) {
    window.history.pushState(null, "", href(n))
    top.current?.scrollIntoView({ block: "start" })
  }
  const f = st.filter
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
      />
      <FilterLayout
        initial={filters}
        label="Filter the archive"
        clear={clear}
        filters={
          <FilterForm action="/archive" label="Filter the archive">
            <FilterGroup title="Reason">
              <Choices
                name="reason"
                label="Reason"
                value={f.reason ?? ""}
                onPick={(reason) => pick({ reason })}
                options={[
                  { value: "", label: "All", count: data?.counts.total },
                  { value: "archived", label: reasons.archived, count: data?.counts.archived },
                  { value: "deprecated", label: reasons.deprecated, count: data?.counts.deprecated },
                ]}
              />
            </FilterGroup>
            <ProductChoices value={f.group ?? ""} onPick={(group) => pick({ group })} />
            <FilterGroup title="Name">
              <TextFilter name="q" label="Name contains" value={f.q ?? ""} onPick={(q) => pick({ q })} />
            </FilterGroup>
            <FilterGroup title="Show">
              <Toggle
                name="used"
                label="Still depended on"
                checked={f.used === true}
                onPick={(on) => pick({ used: on ? "true" : "" })}
              />
            </FilterGroup>
            <FilterGroup title="Order">
              <Choices
                name="sort"
                label="Order"
                value={f.sort ?? "active"}
                onPick={(sort) => pick({ sort })}
                options={[...archiveSorts]}
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
            aria-label="Retired repositories"
            aria-busy={isPlaceholderData}
            className="flex scroll-mt-4 flex-col gap-3"
          >
            <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              <FreshnessBadge freshness={data.freshness} />
              {data.counts.used > 0 && <span>{count(data.counts.used)} still depended on by other repositories.</span>}
            </div>
            <Pager page={data.page} href={href} onPage={goPage} noun="retired repositories" />
            <div className={cn("panel px-3", isPlaceholderData && "opacity-60")}>
              {data.archive.length === 0 ? (
                <EmptyState
                  title={
                    data.counts.total === 0 && !f.q && !f.group
                      ? "No repository has been retired."
                      : "No retired repository matches."
                  }
                />
              ) : (
                <ol aria-label="Archive" className="divide-y divide-border">
                  {data.archive.map((a) => (
                    <ListRow
                      key={a.repo.address}
                      lead={<InitialsAvatar name={a.repo.name} />}
                      title={
                        <Link href={repoPath(a.repo.owner, a.repo.name)} className="min-w-0 break-words hover:underline">
                          {a.repo.address}
                        </Link>
                      }
                      aside={
                        <span className="text-sm text-muted-foreground">
                          last active <RelativeTime at={a.lastActive} />
                        </span>
                      }
                      meta={
                        <>
                          <Badge variant={a.reason === "archived" ? "warn" : "default"}>{reasons[a.reason]}</Badge>
                          {a.usedBy > 0 && <Badge variant="error">used by {count(a.usedBy)}</Badge>}
                          {a.repo.release && <span className="font-mono text-xs">{a.repo.release.tag}</span>}
                          {a.authors > 0 && <span>{plural(a.authors, "author", "authors")}</span>}
                        </>
                      }
                    >
                      {a.repo.description && <p className="text-sm text-muted-foreground">{a.repo.description}</p>}
                    </ListRow>
                  ))}
                </ol>
              )}
            </div>
            {data.archive.length > 0 && (
              <Pager page={data.page} href={href} onPage={goPage} noun="retired repositories" label="Pages, at the end" />
            )}
            {error && <ProblemAlert error={error} />}
          </section>
        )}
      </FilterLayout>
    </>
  )
}
