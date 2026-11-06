import type { Schemas } from "@/lib/api/client"
import { productsViews, remember, viewCookie, workCookie, workOf, type ProductsView } from "@/lib/preferences"

/**
 * WebMCP (design/agents.md, W3): the page offers the browser's agent the portal's tools through `document.modelContext`, the Web
 * Machine Learning Community Group draft of 2 November 2026 (Chrome's earlier preview had it on `navigator`). The catalogue's tools
 * run on the server as the reader (POST /api/v1/tools/{name}); the page adds three of its own. Nothing happens without WebMCP.
 */

/** The draft's tool, as the page registers it. */
export interface ModelContextTool {
  name: string
  title?: string
  description: string
  inputSchema?: object
  annotations?: { readOnlyHint?: boolean; untrustedContentHint?: boolean; consequentialHint?: boolean }
  execute: (input: Record<string, unknown>, options?: { signal?: AbortSignal }) => Promise<unknown>
}

/** The draft's ModelContext: registerTool takes an AbortSignal to unregister; the earlier preview returned an object with unregister. */
export interface ModelContext {
  registerTool: (tool: ModelContextTool, options?: { signal?: AbortSignal }) => unknown
}

/** modelContext is the browser's ModelContext, or none. */
export function modelContext(): ModelContext | undefined {
  const at = (o: unknown) => (o as { modelContext?: ModelContext } | undefined)?.modelContext
  const mc = at(globalThis.document) ?? at(globalThis.navigator)
  return typeof mc?.registerTool === "function" ? mc : undefined
}

/** The page's agent can steer: where it goes and the reader's preferences. */
export interface Page {
  path: () => string
  title: () => string
  open: (path: string) => void
  refresh: () => void
}

/** Kind and subject of a page of the portal, from its path. */
export function pageOf(path: string): Record<string, string> {
  const seg = (path.split("?")[0] ?? "").split("/").filter(Boolean).map(decodeURIComponent)
  const [first, a, b, c] = seg
  if (seg.length === 0) return { kind: "dashboard" }
  if (first === "groups" && a) return { kind: b === "commits" ? "group commits" : "group", group: a }
  if (first === "repos" && a && b) {
    if (c === "commits") return { kind: "repository history", owner: a, name: b }
    if (c === "docs") return { kind: "document", owner: a, name: b, path: seg.slice(4).join("/") }
    return { kind: "repository", owner: a, name: b }
  }
  const named: Record<string, string> = {
    commits: "commits",
    settings: "settings",
    guide: "guide",
    ask: "assistant",
    groups: "groups",
  }
  return { kind: (first && named[first]) || "other" }
}

/** safePath is a path of this portal an agent may open: relative, no scheme, no other host, no admin page. */
export function safePath(p: unknown): string | undefined {
  if (typeof p !== "string" || !p.startsWith("/") || p.startsWith("//") || p.includes("\\") || p.length > 500) return undefined
  const u = new URL(p, "https://portal.invalid")
  if (u.origin !== "https://portal.invalid") return undefined
  // The server reads the path decoded (/%61uth is /auth): the guard reads it as the server does, and a path that does not decode is no path.
  let path = u.pathname
  try {
    for (let i = 0; i < 3 && path.includes("%"); i++) path = decodeURIComponent(path)
  } catch {
    return undefined
  }
  if (path.includes("%") || /^\/(admin|auth)(\/|$)/i.test(path)) return undefined
  return u.pathname + u.search + u.hash
}

async function call(name: string, input: Record<string, unknown>, signal?: AbortSignal) {
  const r = await fetch(`${location.origin}/api/v1/tools/${encodeURIComponent(name)}`, {
    method: "POST",
    headers: { Accept: "application/json", "Content-Type": "application/json" },
    body: JSON.stringify({ input }),
    signal,
  })
  const body = (await r.json().catch(() => ({}))) as { source?: string; text?: string; isError?: boolean; detail?: string }
  if (!r.ok) return { isError: true, text: body.detail ?? `The portal answered ${r.status}.` }
  return { isError: !!body.isError, text: body.text ?? "", source: body.source ? new URL(body.source, location.origin).href : "" }
}

/** pageTools are the page's own tools: what the reader looks at, going to a page, and the reader's preferences. */
export function pageTools(page: Page): ModelContextTool[] {
  return [
    {
      name: "current_page",
      title: "Current page",
      description:
        "Tells what the reader is looking at in the Source Code Portal: the page kind (dashboard, group, repository, document, commits, ...), its path and title, and the repository (owner, name) or group it is about. Use it to resolve 'this repository' or 'this group'.",
      inputSchema: { type: "object", properties: {}, additionalProperties: false },
      annotations: { readOnlyHint: true },
      execute: () => Promise.resolve({ ...pageOf(page.path()), path: page.path(), title: page.title(), url: location.href }),
    },
    {
      name: "open_page",
      title: "Open a page",
      description:
        "Opens a page of the portal for the reader, by its path: / (the dashboard), /groups/{group}, /repos/{owner}/{name}, /repos/{owner}/{name}/commits, /commits, /settings. The tools' answers give paths as their source.",
      inputSchema: {
        type: "object",
        required: ["path"],
        properties: { path: { type: "string", description: "A path of the portal, starting with /" } },
        additionalProperties: false,
      },
      annotations: { readOnlyHint: false, consequentialHint: false },
      execute: (input) => {
        const p = safePath(input.path)
        if (!p) return Promise.resolve({ isError: true, text: "Only a path of this portal, starting with /, opens." })
        page.open(p)
        return Promise.resolve({ opened: p })
      },
    },
    {
      name: "set_preferences",
      title: "Set the reader's preferences",
      description:
        "Sets the reader's preferences, as the Settings page does: work, the work that counts on the dashboard (any of hand, ai, automation), and products, how products are shown (cards or table).",
      inputSchema: {
        type: "object",
        properties: {
          work: {
            type: "array",
            items: { type: "string", enum: ["hand", "ai", "automation"] },
            description: "The kinds of work that count",
          },
          products: { type: "string", enum: productsViews, description: "Cards or one table" },
        },
        additionalProperties: false,
      },
      annotations: { readOnlyHint: false, consequentialHint: false },
      execute: (input) => {
        const set: Record<string, unknown> = {}
        if (input.work !== undefined) {
          const w = workOf(input.work)
          remember(workCookie, w.join(","))
          set.work = w
        }
        if (typeof input.products === "string" && productsViews.includes(input.products as ProductsView)) {
          remember(viewCookie, input.products)
          set.products = input.products
        }
        page.refresh()
        return Promise.resolve({ set })
      },
    },
  ]
}

/** catalogueTools are the server's tools: each call runs on the server, as the reader. */
export function catalogueTools(list: Schemas["ToolList"]): ModelContextTool[] {
  return list.tools.map((t) => ({
    name: t.name,
    title: t.title,
    description: t.description,
    inputSchema: t.inputSchema,
    annotations: { readOnlyHint: t.annotations.readOnly, untrustedContentHint: t.annotations.untrustedContent },
    execute: (input, options) => call(t.name, input ?? {}, options?.signal),
  }))
}

/**
 * register offers the tools to mc until signal aborts. A tool the browser refuses (a name it already holds) is left out; the
 * earlier preview's unregister is called on abort.
 */
export function register(mc: ModelContext, tools: ModelContextTool[], signal: AbortSignal) {
  for (const t of tools) {
    try {
      const r = mc.registerTool(t, { signal }) as { unregister?: () => void } | Promise<unknown> | undefined
      if (r instanceof Promise) r.catch(() => undefined)
      else if (r && typeof r.unregister === "function") signal.addEventListener("abort", () => r.unregister?.(), { once: true })
    } catch {
      // A browser that refuses one tool still gets the others.
    }
  }
}
