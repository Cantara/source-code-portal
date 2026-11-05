import Link from "next/link"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { addressPath } from "@/lib/paths"
import { DiffStat } from "@/entities/commit/diffstat"
import { EmptyState } from "@/shared/empty-state"
import { InitialsAvatar } from "@/shared/initials-avatar"
import { RelativeTime } from "@/shared/relative-time"

export type Pull = Schemas["PullRequest"]
export type PullState = Schemas["PullState"]
export type PullCounts = Schemas["PullCounts"]

/**
 * pullStates are whose turn a pull request is (design/pulls.md), in the order a reader acts on them: the ones waiting for a review
 * first, then the ones ready to merge, then the author's, CI's, drafts and the ones not read yet. Each its words, whose turn and the
 * colour of its dot (theme tokens, never a fixed colour).
 */
export const pullStates: { state: PullState; label: string; waits: string; dot: string }[] = [
  { state: "review", label: "Needs review", waits: "waits for a review", dot: "bg-primary" },
  { state: "ready", label: "Ready to merge", waits: "waits for a maintainer to merge", dot: "bg-[var(--active)]" },
  { state: "changes", label: "Changes requested", waits: "waits for its author", dot: "bg-[var(--quiet)]" },
  { state: "failing", label: "Failing checks", waits: "waits for its author", dot: "bg-[var(--danger)]" },
  { state: "conflicts", label: "Conflicts", waits: "waits for its author", dot: "bg-[var(--danger)]" },
  { state: "checks", label: "Checks running", waits: "waits for CI", dot: "bg-[var(--recent)]" },
  { state: "draft", label: "Draft", waits: "a draft of its author", dot: "bg-muted-foreground" },
  { state: "unknown", label: "Not read yet", waits: "its state is not read yet", dot: "bg-muted-foreground/50" },
]

/** stateOf is a state's words and dot. */
const unread = {
  state: "unknown",
  label: "Not read yet",
  waits: "its state is not read yet",
  dot: "bg-muted-foreground/50",
} as const

export const stateOf = (s: PullState) => pullStates.find((x) => x.state === s) ?? unread

/** PullStateChip is a pull request's state: its dot and its words, with whose turn it is for a screen reader and on hover. */
export function PullStateChip({ state, className }: { state: PullState; className?: string }) {
  const s = stateOf(state)
  return (
    <span
      title={`${s.label}: ${s.waits}`}
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 rounded-full border border-border px-2 py-px text-xs font-medium whitespace-nowrap text-foreground",
        className,
      )}
    >
      <span aria-hidden="true" className={cn("size-1.5 rounded-full", s.dot)} />
      {s.label}
      <span className="sr-only">, {s.waits}</span>
    </span>
  )
}

/** checksWords are the head commit's checks in a word, none when there are none. */
const checksWords: Record<Pull["checks"], string> = {
  passing: "checks pass",
  failing: "checks fail",
  running: "checks running",
  none: "",
}

/**
 * PullList is open pull requests whose turn first, as the server orders them: the state, the title linking to GitHub, the
 * repository and number, who opened it (automation marked), how long it is open and idle, what it changes and its labels. compact
 * keeps each to two short lines for a side panel or a wall.
 */
export function PullList({
  pulls,
  showRepo = true,
  compact = false,
  empty = "No open pull requests",
  label = "Pull requests",
  className,
}: {
  pulls: Pull[]
  showRepo?: boolean
  compact?: boolean
  empty?: string
  /** the list's name for assistive technology */
  label?: string
  className?: string
}) {
  if (pulls.length === 0) return <EmptyState title={empty} />
  return (
    <ol aria-label={label} className={cn("divide-y divide-border", className)}>
      {pulls.map((p) => (
        <li key={`${p.repo}#${p.number}`} className={cn("flex gap-3", compact ? "py-1.5" : "py-2")}>
          {!compact && <InitialsAvatar name={p.author} />}
          <div className="min-w-0 flex-1">
            <p className={cn("flex items-baseline gap-2", compact && "text-sm")}>
              <a
                href={p.url}
                rel="noreferrer"
                className={cn("min-w-0 hover:underline", compact ? "truncate" : "break-words")}
                title={p.title}
              >
                {p.title}
              </a>
              {!compact && p.stats && <DiffStat stats={p.stats} className="ml-auto self-center" />}
            </p>
            <p className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-sm text-muted-foreground">
              <PullStateChip state={p.state} />
              {showRepo ? (
                <Link
                  href={addressPath(p.repo)}
                  className="inline-flex min-h-6 items-center rounded border border-border px-1.5 text-xs font-medium text-foreground hover:bg-accent"
                >
                  {compact ? p.repo.split("/")[1] : p.repo} <span className="text-muted-foreground">#{p.number}</span>
                </Link>
              ) : (
                <span className="font-mono text-xs">#{p.number}</span>
              )}
              <span>
                {p.author}
                {p.bot && <span className="text-xs"> (automation)</span>}
              </span>
              <span aria-hidden="true">·</span>
              <span title="the last activity">
                {!compact && "updated "}
                <RelativeTime at={p.updatedAt} />
              </span>
              {!compact && (
                <>
                  <span aria-hidden="true">·</span>
                  <span>
                    opened <RelativeTime at={p.createdAt} />
                  </span>
                  {checksWords[p.checks] && (
                    <>
                      <span aria-hidden="true">·</span>
                      <span>{checksWords[p.checks]}</span>
                    </>
                  )}
                  {p.labels.slice(0, 3).map((l) => (
                    <span key={l.name} className="rounded-full border border-border px-1.5 text-xs">
                      {l.name}
                    </span>
                  ))}
                </>
              )}
            </p>
          </div>
        </li>
      ))}
    </ol>
  )
}
