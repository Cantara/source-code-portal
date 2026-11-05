"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useState, type FormEvent, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api/client"
import { groupsQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { count } from "@/lib/format"
import { PageSizeField } from "@/shared/pager"

/** QueueStatus is one state of a list of open work (pull requests, issues): its words, what it waits for and its dot. */
export interface QueueStatus<S extends string> {
  state: S
  label: string
  waits: string
  dot: string
}

const chip =
  "inline-flex min-h-8 items-center gap-1.5 rounded-full border border-border px-3 text-sm hover:bg-accent aria-[current=page]:border-primary aria-[current=page]:bg-accent aria-[current=page]:font-medium"

/**
 * QueueChips are a list's states with their counts, each a link that filters to it (to), "All open" first; the current one is
 * marked, a state without any left out unless it is the current one.
 */
export function QueueChips<S extends string>({
  label,
  states,
  open,
  counts,
  current,
  to,
}: {
  label: string
  states: QueueStatus<S>[]
  open: number
  counts: Record<S, number>
  current?: S
  to: (state?: S) => string
}) {
  return (
    <nav aria-label={label}>
      <ul className="flex flex-wrap gap-2">
        <li>
          <Link href={to()} className={chip} aria-current={current ? undefined : "page"}>
            All open <span className="font-mono tabular-nums">{count(open)}</span>
          </Link>
        </li>
        {states
          .filter((s) => counts[s.state] > 0 || current === s.state)
          .map((s) => (
            <li key={s.state}>
              <Link
                href={to(s.state)}
                className={chip}
                aria-current={current === s.state ? "page" : undefined}
                title={`${s.label}: ${s.waits}`}
              >
                <span aria-hidden="true" className={cn("size-2 rounded-full", s.dot)} />
                {s.label} <span className="font-mono tabular-nums">{count(counts[s.state])}</span>
              </Link>
            </li>
          ))}
      </ul>
    </nav>
  )
}

/** queueField is the look of a filter form's select or input. */
export const queueField = "mt-1 min-h-9 rounded-md border border-input bg-background px-2"

/**
 * QueueForm is a list's filters as a GET form that works without JavaScript: the fields of its own first (before), the product, the
 * repository, who opened it and the order (waiting names the default order), those after it, the page size and Apply. With
 * JavaScript onApply gets the form's fields, the repository's text trimmed.
 */
export function QueueForm({
  action,
  label,
  filter,
  waiting,
  before,
  after,
  size,
  onSize,
  onApply,
}: {
  action: string
  label: string
  filter: { group?: string; repo?: string; by?: string; sort?: string; state?: string }
  waiting: string
  before?: ReactNode
  after?: ReactNode
  size: number
  onSize: (n: number) => void
  onApply: (fields: Record<string, string>) => void
}) {
  const groups = useQuery(groupsQuery(api))
  const [repoText, setRepoText] = useState(filter.repo ?? "")
  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const fields: Record<string, string> = {}
    for (const [k, v] of new FormData(e.currentTarget)) if (typeof v === "string") fields[k] = v
    onApply({ ...fields, repo: repoText.trim() })
  }
  return (
    <form onSubmit={submit} action={action} className="flex flex-wrap items-end gap-3 text-sm" aria-label={label}>
      {filter.state && <input type="hidden" name="state" value={filter.state} />}
      {before}
      <label className="flex flex-col">
        Product
        <select name="group" defaultValue={filter.group ?? ""} className={queueField}>
          <option value="">All products</option>
          {groups.data?.groups.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
      </label>
      <label className="flex flex-col">
        Repository (owner/name)
        <input name="repo" value={repoText} onChange={(e) => setRepoText(e.target.value)} className={queueField} />
      </label>
      <label className="flex flex-col">
        Opened by
        <select name="by" defaultValue={filter.by ?? ""} className={queueField}>
          <option value="">People and automation</option>
          <option value="people">People</option>
          <option value="automation">Automation</option>
        </select>
      </label>
      <label className="flex flex-col">
        Order
        <select name="sort" defaultValue={filter.sort ?? "waiting"} className={queueField}>
          <option value="waiting">{waiting}</option>
          <option value="newest">Newest first</option>
          <option value="updated">Updated last first</option>
        </select>
      </label>
      {after}
      <PageSizeField value={size} onSize={onSize} />
      <Button type="submit" variant="outline">
        Apply
      </Button>
    </form>
  )
}
