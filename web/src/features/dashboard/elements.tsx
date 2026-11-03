"use client"

import type { Schemas } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { PanelBoundary } from "@/shared/panel-boundary"
import { FleetMap } from "./fleet-map"
import { LatestCommits } from "./latest"
import { Products } from "./products"
import { Telemetry } from "./telemetry"
import { pick, type Element, type ProductsView } from "./layout"

/** Elements draws a layout's elements in order from the dashboard's data. */
export function Elements({ layout, data, view }: { layout: Element[]; data: Schemas["Dashboard"]; view?: ProductsView }) {
  const now = useNow()
  return (
    <div className="flex flex-col gap-6">
      {layout.map((el, i) => {
        switch (el.kind) {
          case "telemetry":
            return <Telemetry key={i} data={data} now={now} />
          case "fleet-map":
            return <FleetMap key={i} groups={pick(data.groups, el.groups)} now={now} />
          case "products":
            return (
              <Products
                key={`${i}-${el.view}`}
                groups={pick(data.groups, el.groups)}
                repoCount={data.repoCount}
                view={view ?? el.view ?? "cards"}
                now={now}
              />
            )
          case "latest-commits":
            return (
              <PanelBoundary key={i}>
                <LatestCommits people={data.latest} count={el.count ?? 10} />
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
      })}
    </div>
  )
}
