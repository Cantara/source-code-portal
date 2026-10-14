import { HydrationBoundary } from "@tanstack/react-query"
import type { Metadata } from "next"
import { dashboardQuery } from "@/features/dashboard/api"
import { DashboardView } from "@/features/dashboard/view"
import { serverApi } from "@/lib/api/server"
import { load, state } from "@/lib/load"

export const metadata: Metadata = { title: { absolute: "Dashboard · Source Code Portal" }, alternates: { canonical: "/" } }

export default async function DashboardPage() {
  await load(dashboardQuery(await serverApi()))
  return (
    <HydrationBoundary state={state()}>
      <DashboardView />
    </HydrationBoundary>
  )
}
