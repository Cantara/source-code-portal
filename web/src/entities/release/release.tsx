import Link from "next/link"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { addressPath } from "@/lib/paths"
import { Badge } from "@/components/ui/badge"
import { EmptyState } from "@/shared/empty-state"
import { RelativeTime } from "@/shared/relative-time"

export type Release = Schemas["ReleaseEntry"]

/**
 * ReleaseList is releases as the server orders them (design/releases.md): the repository (unless the page is its own), the tag
 * linking to GitHub, a pre-release chip, the title when it says more than the tag, and when and by whom.
 */
export function ReleaseList({
  releases,
  showRepo = true,
  empty = "No releases",
  className,
}: {
  releases: Release[]
  /** the repository's name before the tag; a repository's own page leaves it out */
  showRepo?: boolean
  empty?: string
  className?: string
}) {
  if (releases.length === 0) return <EmptyState title={empty} />
  return (
    <ol aria-label="Releases" className={cn("divide-y divide-border", className)}>
      {releases.map((r) => (
        <li key={`${r.repo}@${r.tag}`} className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 py-1.5 text-sm">
          {showRepo && (
            <Link href={addressPath(r.repo)} className="font-medium underline-offset-2 hover:underline">
              {r.repo}
            </Link>
          )}
          <a href={r.url} className="font-mono underline-offset-2 hover:underline">
            {r.tag}
          </a>
          {r.prerelease && <Badge variant="warn">Pre-release</Badge>}
          {r.title && r.title !== r.tag && <span>{r.title}</span>}
          <span className="text-muted-foreground">
            <RelativeTime at={r.publishedAt} />
            {r.author && <> by {r.author}</>}
            {r.bot && <> (automation)</>}
          </span>
        </li>
      ))}
    </ol>
  )
}
