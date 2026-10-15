import Link from "next/link"
import type { Ref } from "react"
import type { Commit, Schemas } from "@/lib/api/client"
import { addressPath } from "@/lib/paths"
import { InitialsAvatar } from "@/shared/initials-avatar"
import { RelativeTime } from "@/shared/relative-time"
import { EmptyState } from "@/shared/empty-state"
import { GraphCell } from "./graph-cell"

/**
 * CommitList is commits newest first: subject, repository, author and time. With graph (the rows the API laid out for one
 * repository's history, one per commit, and their width) each row draws its part of the git graph, and the subject keeps to one line.
 */
export function CommitList({
  commits,
  showRepo = true,
  graph,
  firstRef,
  repoHref = addressPath,
}: {
  commits: Commit[]
  showRepo?: boolean
  graph?: { rows: (Schemas["GraphRow"] | null)[]; lanes: number }
  firstRef?: Ref<HTMLLIElement>
  /** where a commit's repository links to */
  repoHref?: (address: string) => string
}) {
  const g = graph && graph.lanes > 0 ? graph : undefined
  if (commits.length === 0) return <EmptyState title="No commits yet" />
  return (
    <ol className={g ? undefined : "divide-y divide-border"}>
      {commits.map((c, i) => {
        const row = g?.rows[i] ?? undefined
        return (
          <li
            key={c.repo + c.sha}
            ref={i === 0 ? firstRef : undefined}
            tabIndex={i === 0 ? -1 : undefined}
            className={row ? "flex h-13 items-start gap-2 focus:outline-none" : "flex gap-3 py-2 focus:outline-none"}
          >
            {row && g && <GraphCell row={row} lanes={g.lanes} height={52} nodeY={16} className="h-13" />}
            <span className={row ? "mt-0.5" : undefined}>
              <InitialsAvatar name={c.authorName || c.authorLogin} />
            </span>
            <div className={row ? "min-w-0 flex-1 py-1" : "min-w-0 flex-1"}>
              <p className={row ? "truncate leading-6" : "break-words"} title={row ? c.subject : undefined}>
                {c.url ? (
                  <a href={c.url} className="hover:underline" rel="noreferrer">
                    {c.subject}
                  </a>
                ) : (
                  c.subject
                )}
              </p>
              <p className={row ? "truncate text-sm text-muted-foreground" : "text-sm text-muted-foreground"}>
                {showRepo && (
                  <>
                    <Link
                      href={repoHref(c.repo)}
                      className="rounded border border-border px-1.5 py-px text-xs font-medium text-foreground hover:bg-accent"
                    >
                      {c.repo}
                    </Link>{" "}
                    ·{" "}
                  </>
                )}
                {c.authorName || c.authorLogin} · <RelativeTime at={c.committedAt} /> ·{" "}
                <span className="font-mono">{c.sha.slice(0, 7)}</span>
              </p>
            </div>
          </li>
        )
      })}
    </ol>
  )
}
