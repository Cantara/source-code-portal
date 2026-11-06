"use client"

import type { ReactNode } from "react"
import type { Schemas } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { sideOf, type Side, type Work } from "@/lib/preferences"
import { PanelBoundary } from "@/shared/panel-boundary"
import type { Notice } from "@/entities/announcement/announcement"
import { Announcements } from "./announcements"
import { FleetMap } from "./fleet-map"
import { LatestCommits } from "./latest"
import { Products } from "./products"
import { SidePanel } from "./side"
import { Telemetry } from "./telemetry"
import { kindLabel, pick, type Element, type Kind, type ProductsView } from "./layout"
import { Pulls } from "./pulls"
import { useFeatures } from "@/lib/rules-context"

/** sideKinds are the elements that sit beside the products on a wide screen. */
const sideKinds = new Set<Kind>(["pulls", "latest-commits"])

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
  const draw = (el: Element, i: number, fold?: ReactNode) => {
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
              fold={fold}
            />
          </PanelBoundary>
        )
      case "pulls":
        return features.pulls && <Pulls key={i} work={data.work} counts={data.pulls} n={el.count ?? 5} fold={fold} />
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
    // The pull requests and the latest commits right after the products are their side panel, in order, the first with the fold.
    let end = i + 1
    while (end < layout.length && sideKinds.has(layout[end]?.kind ?? "heading")) end++
    if (el?.kind === "products" && end > i + 1) {
      const start = i
      const beside = layout.slice(i + 1, end)
      out.push(
        <SidePanel
          key={`side-${i}`}
          label={kindLabel((beside.find((x) => x.kind === "latest-commits") ?? beside[0] ?? el).kind)}
          initial={side}
          main={draw(el, i)}
          aside={(fold) => (
            <div className="flex flex-col gap-6">{beside.map((x, j) => draw(x, start + 1 + j, j === 0 ? fold : undefined))}</div>
          )}
        />,
      )
      i = end - 1
    } else if (el) {
      out.push(draw(el, i))
    }
  }
  return <div className="flex flex-col gap-6">{out}</div>
}
