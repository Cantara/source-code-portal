import { HydrationBoundary } from "@tanstack/react-query"
import type { Metadata } from "next"
import { dashboardQuery, reposQuery } from "@/features/dashboard/api"
import { DashboardView } from "@/features/dashboard/view"
import { serverApi } from "@/lib/api/server"
import { load, prefetch, state } from "@/lib/load"

export const metadata: Metadata = { title: { absolute: "Dashboard · Source Code Portal" }, alternates: { canonical: "/" } }

export default async function DashboardPage() {
  const api = await serverApi()
  await Promise.all([load(dashboardQuery(api)), prefetch(reposQuery(api))])
  return (
    <HydrationBoundary state={state()}>
      <DashboardView />
    </HydrationBoundary>
  )
}
