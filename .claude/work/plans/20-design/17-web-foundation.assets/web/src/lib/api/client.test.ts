import { HttpResponse } from "msw"
import { describe, expect, it } from "vitest"
import { createApi, unwrap } from "./client"
import { ProblemError } from "./problem"
import { serverStatus } from "@/lib/server-status"
import { http, server } from "@/test/server"

const api = createApi()
const snapshot = { version: 7, builtAt: "2026-10-14T09:41:07Z", readiness: "ready" as const, buildId: "abc123def456" }

describe("unwrap", () => {
  it("returns data and marks the server reachable", async () => {
    server.use(http.get("/api/v1/snapshot", () => HttpResponse.json(snapshot)))
    expect(await unwrap(api.GET("/api/v1/snapshot"))).toEqual(snapshot)
    expect(serverStatus.get().reachable).toBe(true)
  })
  it("turns an RFC 9457 body into a ProblemError with code and request id", async () => {
    server.use(
      http.get("/api/v1/snapshot", () =>
        HttpResponse.json(
          {
            type: "urn:scp:problem:not-ready",
            title: "Not ready",
            status: 503,
            code: "not-ready",
            instance: "urn:uuid:11111111-2222-3333-4444-555555555555",
          },
          { status: 503, headers: { "Content-Type": "application/problem+json", "Retry-After": "5" } },
        ),
      ),
    )
    const err = await unwrap(api.GET("/api/v1/snapshot")).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ProblemError)
    expect(err).toMatchObject({ status: 503, code: "not-ready", retryAfterSeconds: 5 })
    expect(serverStatus.get().reachable).toBe(false)
  })
  it("classifies a network failure and flags the server unreachable", async () => {
    server.use(http.get("/api/v1/snapshot", () => HttpResponse.error()))
    await expect(unwrap(api.GET("/api/v1/snapshot"))).rejects.toMatchObject({ status: 0, code: "client-network" })
    expect(serverStatus.get().reachable).toBe(false)
  })
  it("does not count a 404 as a server failure", async () => {
    server.use(
      http.get("/api/v1/snapshot", () => HttpResponse.json({ type: "x", title: "t", status: 404, code: "not-found" }, { status: 404 })),
    )
    await expect(unwrap(api.GET("/api/v1/snapshot"))).rejects.toMatchObject({ status: 404 })
    expect(serverStatus.get().reachable).toBe(true)
  })
  it("sets buildSkew when X-SCP-Build differs from the bundle build", async () => {
    server.use(http.get("/api/v1/snapshot", () => HttpResponse.json(snapshot, { headers: { "X-SCP-Build": "other" } })))
    await unwrap(api.GET("/api/v1/snapshot"))
    expect(serverStatus.get().buildSkew).toBe(true)
  })
  it("fails an unmocked request loudly (MSW onUnhandledFrame: error)", async () => {
    await expect(unwrap(api.GET("/api/v1/snapshot"))).rejects.toBeDefined()
  })
  it("a cancelled request is not a server failure (V-02)", async () => {
    server.use(http.get("/api/v1/snapshot", async () => (await new Promise((r) => setTimeout(r, 100)), HttpResponse.json(snapshot))))
    const ac = new AbortController()
    const p = unwrap(api.GET("/api/v1/snapshot", { signal: ac.signal }))
    ac.abort()
    await expect(p).rejects.toMatchObject({ code: "client-aborted" })
    expect(serverStatus.get().reachable).toBe(true)
  })
  it("a 404 after an outage proves the server is reachable again (V-03)", async () => {
    serverStatus.reportFailure()
    server.use(
      http.get("/api/v1/snapshot", () =>
        HttpResponse.json(
          { type: "x", title: "Not found", status: 404, code: "not-found" },
          { status: 404, headers: { "Content-Type": "application/problem+json" } },
        ),
      ),
    )
    await expect(unwrap(api.GET("/api/v1/snapshot"))).rejects.toMatchObject({ code: "not-found" })
    expect(serverStatus.get().reachable).toBe(true)
  })
})
