"use client"

import { useQuery } from "@tanstack/react-query"
import { useRef } from "react"
import { api } from "@/lib/api/client"
import { releasesQuery } from "@/lib/api/queries"
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
import { ProblemAlert } from "@/shared/problem-alert"
import { ReleaseList } from "@/entities/release/release"
import { releaseSorts, releasesSearch, releasesState, releaseSources, releaseVariants, releaseWindows } from "./api"

/**
 * ReleasesView is the recent releases of the reader's view (design/releases.md): the filters in the panel at the left, every choice
 * in view with the variants' counts (the latest of each repository, every release, stable, pre-releases; the product, the
 * repository, the window, the order, the page size), and numbered pages of the reader's size (defaultSize until chosen).
 */
export function ReleasesView({ defaultSize, filters }: { defaultSize: number; filters: Filters }) {
  const { st, href, pick, pickSize, clear } = useListNav({
    list: "releases",
    base: "/releases",
    parse: releasesState,
    serialize: releasesSearch,
    defaultSize,
  })
  const top = useRef<HTMLElement>(null)
  const { data, error, isPlaceholderData } = useQuery(releasesQuery(api, { ...st.filter, page: st.page, size: st.size }))
  function goPage(n: number) {
    window.history.pushState(null, "", href(n))
    top.current?.scrollIntoView({ block: "start" })
  }
  const f = st.filter
  const windows: { value: string; label: string }[] = [...releaseWindows]
  if (f.since && !windows.some((w) => w.value === f.since)) windows.push({ value: f.since, label: `Since ${f.since}` })
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
      />
      <FilterLayout
        initial={filters}
        label="Filter the releases"
        clear={clear}
        filters={
          <FilterForm action="/releases" label="Filter the releases">
            <FilterGroup title="Which releases">
              <Choices
                name="variant"
                label="Which releases"
                value={f.variant ?? "latest"}
                onPick={(variant) => pick({ variant })}
                options={releaseVariants.map((v) => ({ value: v.value, label: v.label, count: data?.counts[v.value] }))}
              />
            </FilterGroup>
            <FilterGroup title="Source">
              <Choices
                name="source"
                label="Source"
                value={f.source ?? "all"}
                onPick={(source) => pick({ source })}
                options={releaseSources.map((x) => ({
                  value: x.value,
                  label: x.label,
                  count: data && x.value !== "all" ? data.counts[x.value] : undefined, // "all" is not a sum: a repository counts once
                }))}
              />
            </FilterGroup>
            <ProductChoices value={f.group ?? ""} onPick={(group) => pick({ group })} />
            <FilterGroup title="Repository">
              <TextFilter name="repo" label="Repository (owner/name)" value={f.repo ?? ""} onPick={(repo) => pick({ repo })} />
            </FilterGroup>
            <FilterGroup title="Published">
              <Choices
                name="since"
                label="Published"
                value={f.since ?? ""}
                onPick={(since) => pick({ since })}
                options={windows}
              />
            </FilterGroup>
            <FilterGroup title="Order">
              <Choices
                name="sort"
                label="Order"
                value={f.sort ?? "newest"}
                onPick={(sort) => pick({ sort })}
                options={[...releaseSorts]}
              />
            </FilterGroup>
            <SizeChoices value={st.size} onPick={pickSize} />
          </FilterForm>
        }
      >
        {error && !data && <ProblemAlert error={error} />}
        {data && (
          <section ref={top} aria-label="Releases" aria-busy={isPlaceholderData} className="flex scroll-mt-4 flex-col gap-3">
            <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              <FreshnessBadge freshness={data.freshness} />
            </div>
            <Pager page={data.page} href={href} onPage={goPage} noun="releases" />
            <div className={cn("panel px-3", isPlaceholderData && "opacity-60")}>
              <ReleaseList
                releases={data.releases}
                empty={data.counts.all === 0 && !f.since ? "No release has been published." : "No release matches."}
              />
            </div>
            {data.releases.length > 0 && (
              <Pager page={data.page} href={href} onPage={goPage} noun="releases" label="Pages, at the end" />
            )}
            {error && <ProblemAlert error={error} />}
          </section>
        )}
      </FilterLayout>
    </>
  )
}
