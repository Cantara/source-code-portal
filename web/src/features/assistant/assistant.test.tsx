import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { http as raw, HttpResponse } from "msw"
import type { ReactNode } from "react"
import { describe, expect, it } from "vitest"
import { setLocation } from "@/test/navigation"
import { http, server } from "@/test/server"
import { Answer, safeHref } from "./markdown"
import { frames, type Turn } from "./stream"
import { history, AskView } from "./view"

function show(view: ReactNode) {
  setLocation("/ask")
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{view}</QueryClientProvider>)
}

const on = { enabled: true, allowed: true, access: "signed-in", provider: "anthropic", model: "claude-haiku-5-5" } as const

function sse(...events: [string, unknown][]) {
  return events.map(([e, d]) => `event: ${e}\ndata: ${JSON.stringify(d)}\n\n`).join("")
}

function body(text: string): ReadableStream<Uint8Array> {
  const bytes = new TextEncoder().encode(text)
  return new ReadableStream({
    start(c) {
      // split across chunks, inside a multi-byte character too
      for (let i = 0; i < bytes.length; i += 7) c.enqueue(bytes.slice(i, i + 7))
      c.close()
    },
  })
}

describe("the stream", () => {
  it("splits events by the WHATWG rules", async () => {
    const out = []
    for await (const f of frames(
      body(': ping\n\nevent: text\r\ndata: {"text":"Blåbær"}\r\n\r\ndata: a\ndata: b\n\nevent: x\n\n'),
    ))
      out.push(f)
    expect(out).toEqual([
      { event: "text", data: '{"text":"Blåbær"}' },
      { event: "message", data: "a\nb" },
    ])
  })
})

describe("the conversation", () => {
  it("sends the answered turns and the question, leaving out failed answers", () => {
    const shown = [
      { role: "user", content: "Q1" },
      { role: "assistant", content: "A1" },
      { role: "user", content: "Q2" },
      { role: "assistant", content: "", failed: "overloaded" },
    ] as const
    expect(history([...shown], "Q3")).toEqual<Turn[]>([
      { role: "user", content: "Q1" },
      { role: "assistant", content: "A1" },
      { role: "user", content: "Q3" },
    ])
  })
})

describe("answers", () => {
  it("keep portal and https links, drop the rest, and never render HTML or images", () => {
    expect(safeHref("/repos/acme/core")).toBe("/repos/acme/core")
    expect(safeHref("//evil.example")).toBeUndefined()
    expect(safeHref("javascript:alert(1)")).toBeUndefined()
    expect(safeHref("http://example.com")).toBeUndefined()
    expect(safeHref("https://example.com/x")).toBe("https://example.com/x")
    const { container } = render(
      <Answer
        text={'[core](/repos/acme/core) [x](javascript:alert(1)) ![pixel](https://evil.example/p.png) <b onclick="x">bold</b>'}
      />,
    )
    expect(screen.getByRole("link", { name: "core" })).toHaveAttribute("href", "/repos/acme/core")
    expect(screen.queryByRole("link", { name: "x" })).toBeNull()
    expect(container.querySelector("img, b, script")).toBeNull()
    expect(container).toHaveTextContent("pixel")
  })
})

describe("AskView", () => {
  it("streams an answer with what the assistant read", async () => {
    let sent: unknown
    server.use(
      http.get("/assistant", ({ response }) => response(200).json(on)),
      raw.post("*/api/v1/assistant/chat", async ({ request }) => {
        sent = await request.json()
        return new HttpResponse(
          body(
            sse(
              ["tool", { name: "get_repository", label: "Reading acme/core" }],
              ["text", { text: "Core is [the platform](/repos/acme/core)." }],
              ["done", { usage: { inputTokens: 10, outputTokens: 5 } }],
            ),
          ),
          { headers: { "Content-Type": "text/event-stream" } },
        )
      }),
    )
    show(<AskView />)
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText("Your question"), "What is core?{Enter}")
    const link = await screen.findByRole("link", { name: "the platform" })
    expect(link).toHaveAttribute("href", "/repos/acme/core")
    expect(within(screen.getByRole("list", { name: "What the assistant read" })).getByText("Reading acme/core")).toBeVisible()
    expect(sent).toEqual({ messages: [{ role: "user", content: "What is core?" }] })
    expect(screen.getByRole("button", { name: "Ask" })).toBeDisabled()
  })

  it("tells a failed answer and keeps a refused question", async () => {
    server.use(
      http.get("/assistant", ({ response }) => response(200).json(on)),
      raw.post("*/api/v1/assistant/chat", () =>
        HttpResponse.json(
          {
            type: "about:blank",
            title: "Too Many Requests",
            status: 429,
            code: "too-many-requests",
            detail: "The assistant is busy.",
          },
          { status: 429, headers: { "Retry-After": "5" } },
        ),
      ),
    )
    show(<AskView />)
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText("Your question"), "Q{Enter}")
    expect(await screen.findByRole("alert")).toHaveTextContent("The assistant is busy.")
    expect(screen.getByLabelText("Your question")).toHaveValue("Q")
  })

  it("asks an anonymous viewer to sign in", async () => {
    server.use(http.get("/assistant", ({ response }) => response(200).json({ ...on, allowed: false })))
    show(<AskView />)
    expect(await screen.findByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/signin?return_to=/ask")
  })
})
