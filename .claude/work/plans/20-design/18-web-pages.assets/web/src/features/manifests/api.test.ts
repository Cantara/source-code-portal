// @vitest-environment jsdom
// (api.ts reaches lib/api/client.ts, which reads window.location at load)
import { describe, expect, it } from "vitest"
import { ProblemError } from "@/lib/api/problem"
import { problemView } from "@/shared/problem-view"
import { assertGraphBody } from "./api"

describe("graph body guard (V-03)", () => {
  it("a text body is a client-contract ProblemError that maps to a server-error page, not to 'offline'", () => {
    let caught: unknown
    try { assertGraphBody("graph TD; a-->b") } catch (e) { caught = e }
    expect(caught).toBeInstanceOf(ProblemError)
    expect(problemView(caught).kind).toBe("server-error")
    expect(assertGraphBody({ nodes: [] })).toEqual({ nodes: [] })
  })
})
