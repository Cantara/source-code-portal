import { cn } from "@/lib/cn"

/** repoBadges are the badges every repository has, in display order (/badges/repos/{owner}/{name}/{kind}.svg). */
export const repoBadges = ["last-commit", "release", "license", "issues"] as const

/** RepoBadges shows a repository's badges; lazy below the fold, as a group table holds many. */
export function RepoBadges({ address, lazy = false, className }: { address: string; lazy?: boolean; className?: string }) {
  return (
    <span className={cn("flex flex-wrap gap-1.5", className)}>
      {repoBadges.map((b) => (
        <img
          key={b}
          src={`/badges/repos/${address}/${b}.svg`}
          alt={`${b.replace("-", " ")} badge`}
          height={20}
          loading={lazy ? "lazy" : undefined}
          className="h-5"
        />
      ))}
    </span>
  )
}

/** GroupBadge shows a group's repository count badge. */
export function GroupBadge({ group }: { group: string }) {
  return <img src={`/badges/groups/${group}/repos.svg`} alt="repositories badge" height={20} className="h-5" />
}
