import Link from "next/link"
import type { Ref } from "react"
import type { Commit } from "@/lib/api/client"
import { addressPath } from "@/lib/paths"
import { InitialsAvatar } from "./initials-avatar"
import { RelativeTime } from "./relative-time"
import { EmptyState } from "./empty-state"

/** CommitList is commits newest first: subject, repository, author and time. */
export function CommitList({
  commits,
  showRepo = true,
  firstRef,
}: {
  commits: Commit[]
  showRepo?: boolean
  firstRef?: Ref<HTMLLIElement>
}) {
  if (commits.length === 0) return <EmptyState title="No commits yet" />
  return (
    <ol className="divide-y divide-border">
      {commits.map((c, i) => {
        return (
          <li
            key={c.repo + c.sha}
            ref={i === 0 ? firstRef : undefined}
            tabIndex={i === 0 ? -1 : undefined}
            className="flex gap-3 py-2 focus:outline-none"
          >
            <InitialsAvatar name={c.authorName || c.authorLogin} />
            <div className="min-w-0 flex-1">
              <p className="break-words">
                {c.url ? (
                  <a href={c.url} className="hover:underline" rel="noreferrer">
                    {c.subject}
                  </a>
                ) : (
                  c.subject
                )}
              </p>
              <p className="text-sm text-muted-foreground">
                {showRepo && (
                  <>
                    <Link href={addressPath(c.repo)} className="font-medium hover:underline">
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
