import Link from "next/link"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { addressPath } from "@/lib/paths"
import { Badge } from "@/components/ui/badge"
import { EmptyState } from "@/shared/empty-state"
import { InitialsAvatar } from "@/shared/initials-avatar"
import { ListRow } from "@/shared/list-row"
import { RelativeTime } from "@/shared/relative-time"

export type Release = Schemas["ReleaseEntry"]

/**
 * ReleaseList is releases as the server orders them (design/releases.md), each a row like a pull request's: the author's avatar, the
 * tag linking to GitHub with the release's title when it says more than the tag, and when it was published at the right; the
 * repository (unless the page is its own), a pre-release chip and who published it below. compact keeps each to one short line for a
 * panel on a repository's or a product's page.
 */
export function ReleaseList({
  releases,
  showRepo = true,
  compact = false,
  empty = "No releases",
  className,
}: {
  releases: Release[]
  /** the repository's name before the tag; a repository's own page leaves it out */
  showRepo?: boolean
  compact?: boolean
  empty?: string
  className?: string
}) {
  if (releases.length === 0) return <EmptyState title={empty} />
  if (!compact)
    return (
      <ol aria-label="Releases" className={cn("divide-y divide-border", className)}>
        {releases.map((r) => (
          <ListRow
            key={`${r.repo}@${r.tag}`}
            lead={<InitialsAvatar name={r.author || r.repo.split("/")[1] || r.repo} />}
            title={
              <span className="min-w-0 break-words">
                <a href={r.url} rel="noreferrer" className="font-mono hover:underline">
                  {r.tag}
                </a>
                {r.title && r.title !== r.tag && <> {r.title}</>}
              </span>
            }
            aside={
              <span className="text-sm text-muted-foreground">
                <RelativeTime at={r.publishedAt} />
              </span>
            }
            meta={
              <>
                {showRepo && (
                  <Link
                    prefetch={false}
                    href={addressPath(r.repo)}
                    className="inline-flex min-h-6 items-center rounded border border-border px-1.5 text-xs font-medium text-foreground hover:bg-accent"
                  >
                    {r.repo}
                  </Link>
                )}
                {r.prerelease && <Badge variant="warn">Pre-release</Badge>}
                {r.author && (
                  <span>
                    {r.author}
                    {r.bot && <span className="text-xs"> (automation)</span>}
                  </span>
                )}
              </>
            }
          >
            {r.summary && <p className="text-sm text-muted-foreground">{r.summary}</p>}
          </ListRow>
        ))}
      </ol>
    )
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
