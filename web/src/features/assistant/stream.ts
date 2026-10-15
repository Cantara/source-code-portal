import { ProblemError } from "@/lib/api/client"

/** Turn is one message of the conversation the browser keeps; the server keeps nothing. */
export interface Turn {
  role: "user" | "assistant"
  content: string
}

/** AskEvent is one event of the answer's stream (internal/assistant/loop.go). */
export type AskEvent =
  | { kind: "text"; text: string }
  | { kind: "tool"; name: string; label: string }
  | { kind: "done"; inputTokens: number; outputTokens: number }
  | { kind: "error"; code: string; message: string }

/** frames splits a server-sent event stream into its event names and data, by the WHATWG rules the server writes. */
export async function* frames(body: ReadableStream<Uint8Array>): AsyncGenerator<{ event: string; data: string }> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buf = ""
  let event = ""
  let data: string[] = []
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) return
      buf += decoder.decode(value, { stream: true })
      let nl: number
      while ((nl = buf.search(/\r\n|\r|\n/)) >= 0) {
        const line = buf.slice(0, nl)
        buf = buf.slice(nl + (buf.startsWith("\r\n", nl) ? 2 : 1))
        if (line === "") {
          if (data.length > 0) yield { event: event || "message", data: data.join("\n") }
          event = ""
          data = []
        } else if (!line.startsWith(":")) {
          const i = line.indexOf(":")
          const field = i < 0 ? line : line.slice(0, i)
          const v = i < 0 ? "" : line.slice(i + 1).replace(/^ /, "")
          if (field === "event") event = v
          else if (field === "data") data.push(v)
        }
      }
    }
  } finally {
    reader.releaseLock()
  }
}

/**
 * ask posts the conversation and yields the answer's events. A refusal before the stream (401, 403, 429, ...) throws a ProblemError;
 * a stream that ends without done or error yields a network error.
 */
export async function* ask(turns: Turn[], signal: AbortSignal): AsyncGenerator<AskEvent> {
  let response: Response
  try {
    response = await globalThis.fetch(`${globalThis.location?.origin ?? "http://localhost"}/api/v1/assistant/chat`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
      body: JSON.stringify({ messages: turns }),
      signal,
    })
  } catch (e) {
    if (e instanceof DOMException && e.name === "AbortError") throw e
    throw new ProblemError(0, "network", "The portal could not be reached.")
  }
  if (!response.ok || !response.body) {
    const p = (await response.json().catch(() => ({}))) as { code?: string; detail?: string }
    throw new ProblemError(
      response.status,
      p.code ?? `http-${response.status}`,
      p.detail ?? response.statusText,
      response.headers.get("X-Request-Id") ?? "",
      Number(response.headers.get("Retry-After") ?? "0") || 0,
    )
  }
  for await (const f of frames(response.body)) {
    const d = JSON.parse(f.data) as Record<string, unknown>
    switch (f.event) {
      case "text":
        yield { kind: "text", text: String(d.text ?? "") }
        break
      case "tool":
        yield { kind: "tool", name: String(d.name ?? ""), label: String(d.label ?? "") }
        break
      case "done": {
        const u = (d.usage ?? {}) as { inputTokens?: number; outputTokens?: number }
        yield { kind: "done", inputTokens: u.inputTokens ?? 0, outputTokens: u.outputTokens ?? 0 }
        return
      }
      case "error":
        yield { kind: "error", code: String(d.code ?? "provider"), message: String(d.message ?? "") }
        return
    }
  }
  yield { kind: "error", code: "network", message: "The answer was cut off; try again." }
}
