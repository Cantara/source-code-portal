import Link from "next/link"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { addressPath } from "@/lib/paths"
import { EmptyState } from "@/shared/empty-state"
import { InitialsAvatar } from "@/shared/initials-avatar"
import { RelativeTime } from "@/shared/relative-time"

export type Issue = Schemas["Issue"]
export type IssueState = Schemas["IssueState"]
export type IssueKind = Schemas["IssueKind"]
export type IssueCounts = Schemas["IssueCounts"]

/** triage is the first state, and the one an unknown state falls back to. */
const triage = {
  state: "triage" as IssueState,
  label: "Needs triage",
  waits: "waits for a maintainer to look at it",
  dot: "bg-primary",
}

/**
 * issueStates are where an issue stands (design/issues.md), in the order a reader acts on them: the ones nobody has looked at, the
 * ones nobody took, the ones someone has. Each its words, what it waits for and the colour of its dot (theme tokens).
 */
export const issueStates: { state: IssueState; label: string; waits: string; dot: string }[] = [
  triage,
  { state: "unassigned", label: "Unassigned", waits: "waits for someone to take it", dot: "bg-[var(--quiet)]" },
  { state: "assigned", label: "Assigned", waits: "someone has it", dot: "bg-[var(--active)]" },
]

/** issueKinds are what an issue asks for: GitHub's issue type, else read from its labels. */
export const issueKinds: { kind: IssueKind; label: string }[] = [
  { kind: "bug", label: "Bug" },
  { kind: "feature", label: "Feature" },
  { kind: "question", label: "Question" },
  { kind: "other", label: "Other" },
]

/** issueStateOf is a state's words and dot. */
export const issueStateOf = (s: IssueState) => issueStates.find((x) => x.state === s) ?? triage

/** IssueStateChip is where an issue stands: its dot and its words, with what it waits for for a screen reader and on hover. */
export function IssueStateChip({ state, className }: { state: IssueState; className?: string }) {
  const s = issueStateOf(state)
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

/**
 * IssueList is open issues what waits first, as the server orders them: the state and kind, the title linking to GitHub, the
 * repository and number, who opened it (automation marked), who has it, the comments, how long it is idle and open, its labels and
 * milestone. compact keeps each to two short lines for a side panel.
 */
export function IssueList({
  issues,
  showRepo = true,
  compact = false,
  empty = "No open issues",
  className,
}: {
  issues: Issue[]
  showRepo?: boolean
  compact?: boolean
  empty?: string
  className?: string
}) {
  if (issues.length === 0) return <EmptyState title={empty} />
  return (
    <ol aria-label="Issues" className={cn("divide-y divide-border", className)}>
      {issues.map((i) => (
        <li key={`${i.repo}#${i.number}`} className={cn("flex gap-3", compact ? "py-1.5" : "py-2")}>
          {!compact && <InitialsAvatar name={i.author} />}
          <div className="min-w-0 flex-1">
            <p className={cn("flex items-baseline gap-2", compact && "text-sm")}>
              <a
                href={i.url}
                rel="noreferrer"
                className={cn("min-w-0 hover:underline", compact ? "min-h-6 truncate leading-6" : "break-words")}
                title={i.title}
              >
                {i.title}
              </a>
            </p>
            <p className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-sm text-muted-foreground">
              <IssueStateChip state={i.state} />
              {i.kind !== "other" && (
                <span className="rounded-full border border-border px-1.5 text-xs">
                  {issueKinds.find((k) => k.kind === i.kind)?.label}
                </span>
              )}
              {showRepo ? (
                <Link
                  href={addressPath(i.repo)}
                  className="inline-flex min-h-6 items-center rounded border border-border px-1.5 text-xs font-medium text-foreground hover:bg-accent"
                >
                  {compact ? i.repo.split("/")[1] : i.repo} <span className="text-muted-foreground">#{i.number}</span>
                </Link>
              ) : (
                <span className="font-mono text-xs">#{i.number}</span>
              )}
              <span>
                {i.author}
                {i.bot && <span className="text-xs"> (automation)</span>}
              </span>
              {i.assigned > 0 && (
                <>
                  <span aria-hidden="true">·</span>
                  <span>
                    for {i.assignees.join(", ")}
                    {i.assigned > i.assignees.length && ` +${i.assigned - i.assignees.length}`}
                  </span>
                </>
              )}
              <span aria-hidden="true">·</span>
              <span>
                idle <RelativeTime at={i.updatedAt} />
              </span>
              {!compact && (
                <>
                  <span aria-hidden="true">·</span>
                  <span>
                    opened <RelativeTime at={i.createdAt} />
                  </span>
                  {i.comments > 0 && (
                    <>
                      <span aria-hidden="true">·</span>
                      <span>{i.comments === 1 ? "1 comment" : `${i.comments} comments`}</span>
                    </>
                  )}
                  {i.milestone && (
                    <span className="rounded-full border border-dashed border-border px-1.5 text-xs" title="Milestone">
                      {i.milestone}
                    </span>
                  )}
                  {i.labels.slice(0, 3).map((l) => (
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
