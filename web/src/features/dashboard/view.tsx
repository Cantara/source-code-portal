"use client"

import { useQuery } from "@tanstack/react-query"
import { useState } from "react"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api/client"
import { meQuery } from "@/lib/api/queries"
import { plural } from "@/lib/format"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"
import { dashboardQuery, pageQuery } from "./api"
import { Designer } from "./design"
import { Elements } from "./elements"
import { team, type Side, type Work } from "@/lib/preferences"
import { defaultLayout, type ProductsView } from "./layout"

/**
 * DashboardView is the landing page: its layout's elements in order (design/layout.md), each answering which product needs a look
 * and why, through the reader's lens (work). view is the reader's remembered products view, when there is one. An admin can switch to the design mode.
 */
export function DashboardView({ work = team, view, side }: { work?: Work; view?: ProductsView; side?: Side }) {
  const { data, error } = useQuery(dashboardQuery(api, work))
  const { data: page } = useQuery(pageQuery(api))
  const { data: me } = useQuery(meQuery(api))
  const [designing, setDesigning] = useState(false)
  const layout = page?.elements ?? defaultLayout
  return (
    <>
      <PageHeader
        title="Dashboard"
        meta={
          <>
            {data && (
              <span className="font-mono text-xs text-muted-foreground">
                {plural(data.groups.length, "product", "products")} · {plural(data.repoCount, "repository", "repositories")}
              </span>
            )}
            {me?.admin && !designing && (
              <Button variant="outline" className="ml-auto min-h-8 px-2 text-xs" onClick={() => setDesigning(true)}>
                Design this page
              </Button>
            )}
          </>
        }
      />
      {error && !data && <ProblemAlert error={error} />}
      {data &&
        (designing ? (
          <Designer data={data} groups={data.groups} onClose={() => setDesigning(false)} />
        ) : (
          <Elements layout={layout} data={data} view={view} side={side} />
        ))}
    </>
  )
}
