"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { Alert } from "@/components/ui/alert"
import { Button, buttonClass } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { api, ProblemError } from "@/lib/api/client"
import { assistantQuery } from "@/lib/api/queries"
import { PageHeader } from "@/shared/page-header"
import { ProblemAlert } from "@/shared/problem-alert"
import { Answer } from "./markdown"
import { ask, type Turn } from "./stream"

/** Shown is a message on the page: a turn, with what the assistant read and how its answer ended. */
interface Shown extends Turn {
  tools?: string[]
  failed?: string
}

const field =
  "min-h-20 w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"

/** history is what the server is sent: answered turns only, the failed and the empty left out with their question. */
export function history(shown: Shown[], question: string): Turn[] {
  const out: Turn[] = []
  for (let i = 0; i + 1 < shown.length; i += 2) {
    const q = shown[i]
    const a = shown[i + 1]
    if (q && a && !a.failed && a.content)
      out.push({ role: "user", content: q.content }, { role: "assistant", content: a.content })
  }
  return [...out, { role: "user", content: question }]
}

/** AskView is the assistant: a conversation kept in the page, each answer streamed with what the assistant read. */
export function AskView() {
  const { data: status, error } = useQuery(assistantQuery(api))
  const [shown, setShown] = useState<Shown[]>([])
  const [question, setQuestion] = useState("")
  const [refused, setRefused] = useState<unknown>()
  const abort = useRef<AbortController | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => () => abort.current?.abort(), [])

  const update = (fn: (a: Shown) => Shown) =>
    setShown((s) => [...s.slice(0, -1), fn(s.at(-1) ?? { role: "assistant", content: "" })])

  async function submit(e?: FormEvent) {
    e?.preventDefault()
    const q = question.trim()
    if (!q || abort.current) return
    const turns = history(shown, q)
    const ctl = new AbortController()
    abort.current = ctl
    setBusy(true)
    setRefused(undefined)
    setQuestion("")
    setShown((s) => [...s, { role: "user", content: q }, { role: "assistant", content: "", tools: [] }])
    try {
      for await (const ev of ask(turns, ctl.signal)) {
        if (ev.kind === "text") update((a) => ({ ...a, content: a.content + ev.text }))
        else if (ev.kind === "tool") update((a) => ({ ...a, tools: [...(a.tools ?? []), ev.label] }))
        else if (ev.kind === "error") update((a) => ({ ...a, failed: ev.message }))
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") update((a) => ({ ...a, failed: "Stopped." }))
      else {
        setShown((s) => s.slice(0, -2))
        setQuestion(q)
        setRefused(err)
      }
    } finally {
      abort.current = null
      setBusy(false)
    }
  }

  function onKey(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) void submit()
    if (e.key === "Enter" && !e.shiftKey) e.preventDefault()
  }

  if (error) return <ProblemAlert error={error} />
  if (!status) return null
  if (!status.enabled || !status.allowed) {
    return (
      <>
        <PageHeader title="Ask" />
        <Alert role="alert">
          <p>
            {!status.enabled
              ? "The assistant is not set up on this portal."
              : status.access === "admin"
                ? "The assistant is for the portal's administrators."
                : "Sign in to ask the assistant."}
          </p>
          {status.enabled && status.access === "signed-in" && (
            <Link href="/signin?return_to=/ask" className={buttonClass("default", "mt-3")}>
              Sign in
            </Link>
          )}
        </Alert>
      </>
    )
  }
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader title="Ask">
        <p className="text-sm text-muted-foreground">
          Questions about the repositories, groups, documents, commits and health this portal knows. Answers come from{" "}
          {status.model} and can be wrong; the conversation is kept in this page only.
        </p>
      </PageHeader>
      <ol className="flex flex-col gap-3" aria-live="polite" aria-busy={busy}>
        {shown.map((m, i) =>
          m.role === "user" ? (
            <li key={i} className="self-end rounded-lg bg-accent px-3 py-2 text-sm whitespace-pre-wrap">
              <span className="sr-only">You asked: </span>
              {m.content}
            </li>
          ) : (
            <li key={i}>
              <Card>
                <span className="sr-only">The assistant answered: </span>
                {m.tools && m.tools.length > 0 && (
                  <ul className="mb-2 flex flex-wrap gap-1 text-xs text-muted-foreground" aria-label="What the assistant read">
                    {m.tools.map((t, j) => (
                      <li key={j} className="rounded border border-border px-1.5 py-0.5">
                        {t}
                      </li>
                    ))}
                  </ul>
                )}
                {m.content ? (
                  <Answer text={m.content} />
                ) : (
                  !m.failed && <p className="text-sm text-muted-foreground">Thinking…</p>
                )}
                {m.failed && <p className="mt-2 text-sm text-destructive">{m.failed}</p>}
              </Card>
            </li>
          ),
        )}
      </ol>
      {refused !== undefined && (
        <div className="mt-3">
          {refused instanceof ProblemError && refused.status === 429 ? (
            <Alert role="alert">{refused.message}</Alert>
          ) : (
            <ProblemAlert error={refused} />
          )}
        </div>
      )}
      <form onSubmit={(e) => void submit(e)} className="mt-4 flex flex-col gap-2">
        <label htmlFor="question" className="text-sm font-medium">
          Your question
        </label>
        <textarea
          id="question"
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          onKeyDown={onKey}
          className={field}
          placeholder="Which repositories use the core library?"
        />
        <div className="flex gap-2">
          {busy ? (
            <Button variant="outline" onClick={() => abort.current?.abort()}>
              Stop
            </Button>
          ) : (
            <Button type="submit" disabled={!question.trim()}>
              Ask
            </Button>
          )}
          {shown.length > 0 && !busy && (
            <Button variant="ghost" onClick={() => setShown([])}>
              New conversation
            </Button>
          )}
        </div>
      </form>
    </div>
  )
}
