"use client"

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { plural } from "@/lib/format"
import { PageHeader } from "@/shared/page-header"
import { PanelBoundary } from "@/shared/panel-boundary"
import { ProblemAlert } from "@/shared/problem-alert"
import { dashboardQuery } from "./api"
import { FleetMap } from "./fleet-map"
import { LatestCommits } from "./latest"
import { Products } from "./products"
import { Telemetry } from "./telemetry"
import { defaultLayout, pick, type Element, type ProductsView } from "./layout"

/**
 * DashboardView is the landing page: its elements in layout order (design/layout.md), each answering which product needs a look
 * and why. view is the reader's remembered products view, when there is one.
 */
export function DashboardView({ layout = defaultLayout, view }: { layout?: Element[]; view?: ProductsView }) {
  const { data, error } = useQuery(dashboardQuery(api))
  const now = useNow()
  return (
    <>
      <PageHeader
        title="Dashboard"
        meta={
          data && (
            <span className="font-mono text-xs text-muted-foreground">
              {plural(data.groups.length, "product", "products")} · {plural(data.repoCount, "repository", "repositories")}
            </span>
          )
        }
      />
      {error && !data && <ProblemAlert error={error} />}
      {data && (
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
                    key={i}
                    groups={pick(data.groups, el.groups)}
                    repoCount={data.repoCount}
                    view={view ?? el.view}
                    now={now}
                  />
                )
              case "latest-commits":
                return (
                  <PanelBoundary key={i}>
                    <LatestCommits people={data.latest} count={el.count} />
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
      )}
    </>
  )
}
