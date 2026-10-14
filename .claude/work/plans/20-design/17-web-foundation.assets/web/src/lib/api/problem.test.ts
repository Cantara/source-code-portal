import { describe, expect, it } from "vitest"
import { isTransient, problemFromResponse, problemFromThrown, ProblemError } from "./problem"

const res = (status: number, headers: Record<string, string> = {}) => new Response(null, { status, headers })

describe("problemFromResponse", () => {
  it("reads code, request id (from instance), retry-after and field errors from a problem document", () => {
    const p = problemFromResponse(res(400), {
      type: "urn:scp:problem:validation-failed",
      title: "Validation failed",
      status: 400,
      code: "validation-failed",
      instance: "urn:uuid:3a8d4c77-6e0b-41f5-9c0d-2b7e5f1a9c68",
      errors: [{ pointer: "#/query/limit", detail: "must be at most 200" }],
    })
    expect(p).toMatchObject({ status: 400, code: "validation-failed", requestId: "3a8d4c77-6e0b-41f5-9c0d-2b7e5f1a9c68" })
    expect(p.fieldErrors).toEqual([{ pointer: "#/query/limit", detail: "must be at most 200" }])
  })
  it("falls back to X-Request-Id and Retry-After when the body lacks them", () => {
    const p = problemFromResponse(res(503, { "X-Request-Id": "abc", "Retry-After": "15" }), {
      type: "x",
      title: "t",
      status: 503,
      code: "not-ready",
    })
    expect(p).toMatchObject({ requestId: "abc", retryAfterSeconds: 15 })
  })
  it("maps a non-problem body (proxy HTML 502) to client-unparseable and keeps the status", () => {
    const p = problemFromResponse(res(502), undefined)
    expect(p).toMatchObject({ status: 502, code: "client-unparseable" })
  })
})

describe("problemFromThrown", () => {
  it("classifies fetch rejections and aborts", () => {
    expect(problemFromThrown(new TypeError("Failed to fetch"))).toMatchObject({ status: 0, code: "client-network" })
    expect(problemFromThrown(new DOMException("x", "AbortError"))).toMatchObject({ code: "client-aborted" })
  })
  it("passes a ProblemError through unchanged", () => {
    const e = new ProblemError({ status: 404, code: "not-found", title: "Not found" })
    expect(problemFromThrown(e)).toBe(e)
  })
})

describe("isTransient", () => {
  it.each([
    [503, "not-ready", true],
    [500, "internal", true],
    [429, "rate-limited", true],
    [0, "client-network", true],
    [404, "not-found", false],
    [400, "validation-failed", false],
    [501, "x", false],
    [0, "client-aborted", false],
    [0, "client-contract", false],
  ])("status %s code %s -> %s", (status, code, want) => {
    expect(isTransient(new ProblemError({ status, code, title: "t" }))).toBe(want)
  })
  it("classifies an AbortError by name, whatever realm created it (V-02)", () => {
    const foreign = Object.assign(new Error("This operation was aborted"), { name: "AbortError" })
    expect(problemFromThrown(foreign).code).toBe("client-aborted")
  })
})
