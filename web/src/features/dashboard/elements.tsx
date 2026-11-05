"use client"

import type { ReactNode } from "react"
import type { Schemas } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { sideOf, type Side, type Work } from "@/lib/preferences"
import { PanelBoundary } from "@/shared/panel-boundary"
import { FleetMap } from "./fleet-map"
import { LatestCommits } from "./latest"
import { Products } from "./products"
import { SidePanel } from "./side"
import { Telemetry } from "./telemetry"
import { pick, type Element, type ProductsView } from "./layout"

/**
 * Elements draws a layout's elements in order from the dashboard's data. The latest commits right after the products become their
 * side panel on a wide screen (side: the reader's remembered panel).
 */
export function Elements({
  layout,
  data,
  view,
  side = sideOf(undefined),
  show,
}: {
  layout: Element[]
  data: Schemas["Dashboard"]
  view?: ProductsView
  side?: Side
  /** the kinds the reader shows in the latest commits (the cookie), if any */
  show?: Work
}) {
  const now = useNow()
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
    const next = layout[i + 1]
    if (el?.kind === "products" && next?.kind === "latest-commits") {
      const at = i + 1
      out.push(
        <SidePanel
          key={`side-${i}`}
          label="Latest commits"
          initial={side}
          main={draw(el, i)}
          aside={(fold) => draw(next, at, fold)}
        />,
      )
      i++
    } else if (el) {
      out.push(draw(el, i))
    }
  }
  return <div className="flex flex-col gap-6">{out}</div>
}
