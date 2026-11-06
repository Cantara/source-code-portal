"use client"

import type { ReactNode } from "react"
import type { Schemas } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { sectionKinds, sideOf, type SectionKind, type Side, type Work } from "@/lib/preferences"
import { PanelBoundary } from "@/shared/panel-boundary"
import type { Notice } from "@/entities/announcement/announcement"
import { Announcements } from "./announcements"
import { FleetMap } from "./fleet-map"
import { LatestCommits } from "./latest"
import { Products } from "./products"
import { SidePanel, type Section } from "./side"
import { Telemetry } from "./telemetry"
import { pick, type Element, type Kind, type ProductsView } from "./layout"
import { Pulls } from "./pulls"
import { Issues } from "./issues"
import { useFeatures } from "@/lib/rules-context"

/** sideKinds are the elements that sit beside the products on a wide screen, each a section of their side panel. */
const sideKinds = new Set<Kind>(sectionKinds)

/**
 * Elements draws a layout's elements in order from the dashboard's data. The pull requests and the latest commits right after the
 * products become their side panel on a wide screen (side: the reader's remembered panel).
 */
export function Elements({
  layout,
  data,
  view,
  side = sideOf(undefined),
  show,
  notices = [],
  dismissed,
}: {
  layout: Element[]
  data: Schemas["Dashboard"]
  view?: ProductsView
  side?: Side
  /** the kinds the reader shows in the latest commits (the cookie), if any */
  show?: Work
  /** the announcements the server gave this page, and the ones the reader dismissed */
  notices?: Notice[]
  dismissed?: string[]
}) {
  const now = useNow()
  const features = useFeatures()
  const on = (el: Element) => (el.kind !== "pulls" || features.pulls) && (el.kind !== "issues" || features.issues)
  const draw = (el: Element, i: number, section?: Section) => {
    switch (el.kind) {
      case "telemetry":
        return <Telemetry key={i} data={data} />
      case "fleet-map":
        return <FleetMap key={i} groups={pick(data.groups, el.groups)} work={data.work} now={now} color={el.color} />
      case "products":
        return (
          <Products
            key={`${i}-${el.view}`}
            groups={pick(data.groups, el.groups)}
            repoCount={data.repoCount}
            view={view ?? el.view ?? "cards"}
            work={data.work}
            now={now}
          />
        )
      case "latest-commits":
        return (
          <PanelBoundary key={i}>
            <LatestCommits
              counted={data.latest}
              work={data.work}
              show={show}
              activity={data.activity}
              count={el.count ?? 10}
              section={section}
            />
          </PanelBoundary>
        )
      case "pulls":
        return on(el) && <Pulls key={i} work={data.work} counts={data.pulls} n={el.count ?? 5} section={section} />
      case "issues":
        return on(el) && <Issues key={i} work={data.work} n={el.count ?? 5} section={section} />
      case "announcements":
        return <Announcements key={i} list={notices} dismissed={dismissed} />
      case "heading":
        return (
          <div key={i}>
            <h2 className="text-lg font-semibold">{el.text}</h2>
            {el.note && <p className="text-sm text-muted-foreground">{el.note}</p>}
          </div>
        )
    }
  }
  const out: ReactNode[] = []
  for (let i = 0; i < layout.length; i++) {
    const el = layout[i]
    // The pull requests, the issues and the latest commits right after the products are the sections of their side panel, in order.
    let end = i + 1
    while (end < layout.length && sideKinds.has(layout[end]?.kind ?? "heading")) end++
    if (el?.kind === "products" && end > i + 1) {
      const start = i
      const beside = layout
        .slice(i + 1, end)
        .map((x, j) => ({ x, at: start + 1 + j }))
        .filter(({ x }) => on(x))
      out.push(
        <SidePanel
          key={`side-${i}`}
          initial={side}
          main={draw(el, i)}
          sections={beside.map(({ x, at }) => ({
            kind: x.kind as SectionKind,
            render: (section: Section) => draw(x, at, section),
          }))}
        />,
      )
      i = end - 1
    } else if (el) {
      out.push(draw(el, i))
    }
  }
  return <div className="flex flex-col gap-6">{out}</div>
}
