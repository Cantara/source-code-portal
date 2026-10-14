import { QueryClientProvider } from "@tanstack/react-query"
import { render, waitFor } from "@testing-library/react"
import { HttpResponse } from "msw"
import { describe, expect, it } from "vitest"
import { createQueryClient, retryPolicy } from "./query-client"
import { invalidateTopics, useSnapshotSync } from "./snapshot"
import { ProblemError } from "./problem"
import { http, server } from "@/test/server"

function Probe() {
  useSnapshotSync()
  return null
}

describe("useSnapshotSync", () => {
  it("invalidates every non-snapshot query when the snapshot version changes", async () => {
    let version = 1
    server.use(
      http.get("/api/v1/snapshot", () =>
        HttpResponse.json({ version, builtAt: "2026-10-14T09:41:07Z", readiness: "ready", buildId: "abc123def456" }),
      ),
    )
    const qc = createQueryClient()
    qc.setQueryData(["catalog", "groups", {}], { x: 1 })
    render(
      <QueryClientProvider client={qc}>
        <Probe />
      </QueryClientProvider>,
    )
    await waitFor(() => expect(qc.getQueryData(["snapshot"])).toBeDefined())
    expect(qc.getQueryState(["catalog", "groups", {}])?.isInvalidated).toBe(false)
    version = 2
    await qc.refetchQueries({ queryKey: ["snapshot"] })
    await waitFor(() => expect(qc.getQueryState(["catalog", "groups", {}])?.isInvalidated).toBe(true))
  })
})

describe("invalidateTopics", () => {
  it("maps topics to query roots, including the composite repo root, and leaves others alone", async () => {
    const qc = createQueryClient()
    for (const k of [
      ["catalog", "groups", {}],
      ["repo", "a"],
      ["pages", "list"],
      ["commits", "list", {}],
    ])
      qc.setQueryData(k, 1)
    await invalidateTopics(qc, ["catalog"])
    expect(qc.getQueryState(["catalog", "groups", {}])?.isInvalidated).toBe(true)
    expect(qc.getQueryState(["repo", "a"])?.isInvalidated).toBe(true)
    expect(qc.getQueryState(["pages", "list"])?.isInvalidated).toBe(false)
    expect(qc.getQueryState(["commits", "list", {}])?.isInvalidated).toBe(false)
  })
})

describe("retryPolicy", () => {
  it("retries transient problems twice (3 attempts in all) and never 4xx", () => {
    const t = new ProblemError({ status: 503, code: "not-ready", title: "t" })
    const c = new ProblemError({ status: 404, code: "not-found", title: "t" })
    expect(retryPolicy(0, t)).toBe(true)
    expect(retryPolicy(1, t)).toBe(true)
    expect(retryPolicy(2, t)).toBe(false)
    expect(retryPolicy(0, c)).toBe(false)
  })
})
