import { render, waitFor } from "@testing-library/react"
import { http as raw, HttpResponse } from "msw"
import { afterEach, describe, expect, it } from "vitest"
import { setLocation } from "@/test/navigation"
import { http, server } from "@/test/server"
import { WebMCP } from "./agents"
import { pageOf, safePath, type ModelContext, type ModelContextTool } from "./webmcp"

/** fake is a draft ModelContext: it holds the registered tools until their signal aborts, and refuses a name it holds. */
function fake() {
  const tools = new Map<string, ModelContextTool>()
  const mc: ModelContext = {
    registerTool: (t, o) => {
      if (tools.has(t.name)) return Promise.reject(new DOMException("taken", "InvalidStateError"))
      tools.set(t.name, t)
      o?.signal?.addEventListener("abort", () => tools.delete(t.name))
      return Promise.resolve()
    },
  }
  Object.defineProperty(document, "modelContext", { value: mc, configurable: true })
  return tools
}

const list = {
  tools: [
    {
      name: "search",
      title: "Search the portal",
      description: "Searches what the portal holds.",
      inputSchema: { type: "object", properties: { query: { type: "string" } } },
      annotations: { readOnly: true, untrustedContent: true },
    },
  ],
}

/** run calls a registered tool, failing the test when it is not there. */
function run(tools: Map<string, ModelContextTool>, name: string, input: Record<string, unknown>) {
  const t = tools.get(name)
  if (!t) throw new Error(`${name} is not registered`)
  return t.execute(input)
}

afterEach(() => {
  Reflect.deleteProperty(document, "modelContext")
  document.cookie = "scp-work=; Path=/; Max-Age=0"
})

describe("WebMCP", () => {
  it("does nothing, and asks the server nothing, without a modelContext", async () => {
    let asked = false
    server.use(
      http.get("/tools", ({ response }) => {
        asked = true
        return response(200).json(list)
      }),
    )
    render(<WebMCP />)
    await new Promise((r) => setTimeout(r, 20))
    expect(asked).toBe(false)
  })

  it("registers the catalogue and the page's tools, runs a call on the server, and unregisters them all when it goes", async () => {
    const tools = fake()
    let sent: unknown
    server.use(
      http.get("/tools", ({ response }) => response(200).json(list)),
      raw.post("*/api/v1/tools/search", async ({ request }) => {
        sent = await request.json()
        return HttpResponse.json({
          source: "/repos",
          text: '<untrusted-content id="n">acme/core</untrusted-content id="n">',
          isError: false,
        })
      }),
    )
    const view = render(<WebMCP />)
    await waitFor(() => expect([...tools.keys()].sort()).toEqual(["current_page", "open_page", "search", "set_preferences"]))
    const search = tools.get("search")
    expect(search?.annotations).toEqual({ readOnlyHint: true, untrustedContentHint: true })
    const r = (await run(tools, "search", { query: "core" })) as { text: string; source: string; isError: boolean }
    expect(sent).toEqual({ input: { query: "core" } })
    expect(r.isError).toBe(false)
    expect(r.text).toContain("acme/core")
    expect(r.source).toBe(`${location.origin}/repos`)
    view.unmount()
    expect(tools.size).toBe(0)
  })

  it("tells the page the reader is on, opens only the portal's own pages and sets the reader's preferences", async () => {
    const tools = fake()
    server.use(http.get("/tools", ({ response }) => response(200).json({ tools: [] })))
    setLocation("/repos/acme/core")
    render(<WebMCP />)
    await waitFor(() => expect(tools.has("current_page")).toBe(true))
    expect(await run(tools, "current_page", {})).toMatchObject({ kind: "repository", owner: "acme", name: "core" })
    expect(await run(tools, "open_page", { path: "https://evil.example/" })).toMatchObject({ isError: true })
    expect(await run(tools, "open_page", { path: "/groups/iam" })).toEqual({ opened: "/groups/iam" })
    await waitFor(async () => expect(await run(tools, "current_page", {})).toMatchObject({ kind: "group", group: "iam" }))
    expect(await run(tools, "set_preferences", { work: ["hand"], products: "table" })).toEqual({
      set: { work: ["hand"], products: "table" },
    })
    expect(document.cookie).toContain("scp-work=hand")
    expect(document.cookie).toContain("scp-view-products=table")
  })
})

describe("pages", () => {
  it("names the page kind and its subject", () => {
    expect(pageOf("/")).toEqual({ kind: "dashboard" })
    expect(pageOf("/groups/iam/commits")).toEqual({ kind: "group commits", group: "iam" })
    expect(pageOf("/repos/acme/core/docs/docs/setup.md")).toEqual({
      kind: "document",
      owner: "acme",
      name: "core",
      path: "docs/setup.md",
    })
    expect(pageOf("/repos/acme/core/commits?page=2")).toEqual({ kind: "repository history", owner: "acme", name: "core" })
    expect(pageOf("/settings")).toEqual({ kind: "settings" })
  })

  it("opens only relative paths of the portal, never an admin page", () => {
    for (const bad of [
      "https://evil.example",
      "//evil.example/x",
      "/\\evil.example",
      "javascript:alert(1)",
      "/admin",
      "/auth/logout",
      "/%61uth/logout",
      "/%61dmin",
      "/%2561dmin",
      "/ADMIN/x",
      "/%zz",
      7,
    ])
      expect(safePath(bad)).toBeUndefined()
    expect(safePath("/repos/acme/core?x=1#top")).toBe("/repos/acme/core?x=1#top")
  })
})
