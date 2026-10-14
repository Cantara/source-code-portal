import type { Repo } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { ago, count, exactUTC } from "@/lib/format"
import { activities, activity, lastChange, type Activity, type Pulse } from "@/lib/health"
import { cn } from "@/lib/cn"

const label = Object.fromEntries(activities.map((a) => [a.level, a])) as Record<Activity, (typeof activities)[number]>

/** ActivityLight is a repository's activity as a coloured light with its label for screen readers. */
export function ActivityLight({ repo, className }: { repo: Repo; className?: string }) {
  const level = activity(repo, useNow())
  return (
    <span className={cn(`lvl-${level} led led-live`, className)} title={`${label[level].label}: ${label[level].within}`}>
      <span className="sr-only">{label[level].label}</span>
    </span>
  )
}

/**
 * HealthBadges are a repository's health at a glance: activity and age, latest release, licence, open issues and language.
 * Every badge carries its meaning in text, so colour is never the only signal.
 */
export function HealthBadges({ repo, className }: { repo: Repo; className?: string }) {
  const now = useNow()
  const level = activity(repo, now)
  const licensed = repo.license !== "" && repo.license !== "NOASSERTION"
  return (
    <span className={cn("flex flex-wrap items-center gap-1", className)}>
      <span className={`lvl-${level} chip`} title={`${label[level].label}: last change ${exactUTC(lastChange(repo))}`}>
        <span aria-hidden="true" className="led size-1.5" />
        <span className="sr-only">{label[level].label}, last change</span>
        <time dateTime={lastChange(repo)} suppressHydrationWarning>
          {ago(lastChange(repo), now)}
        </time>
      </span>
      {repo.release ? (
        <a
          href={repo.release.url}
          rel="noreferrer nofollow"
          className="lvl-recent chip max-w-40 hover:underline"
          title={`Latest release ${repo.release.tag}, ${exactUTC(repo.release.publishedAt)}`}
        >
          <span className="sr-only">Release</span>
          <span className="truncate">{repo.release.tag}</span>
        </a>
      ) : (
        <span className="lvl-dormant chip" title="No release published">
          no release
        </span>
      )}
      <span className={cn("chip", licensed ? "lvl-dormant" : "lvl-danger")} title={licensed ? "Licence" : "No licence detected"}>
        {licensed ? repo.license : "no licence"}
      </span>
      <span className={cn("chip", repo.openIssues > 20 ? "lvl-quiet" : "lvl-dormant")} title="Open issues and pull requests">
        {count(repo.openIssues)}
        <span className="sr-only"> open</span> issues
      </span>
      {repo.language && <span className="chip">{repo.language}</span>}
    </span>
  )
}

/** HealthStrip is one cell per repository, coloured by activity, newest change first; the label says the counts. */
export function HealthStrip({ repos, pulse, className }: { repos: Repo[]; pulse: Pulse; className?: string }) {
  const now = useNow()
  const summary = activities
    .filter((a) => pulse.levels[a.level] > 0)
    .map((a) => `${pulse.levels[a.level]} ${a.label.toLowerCase()}`)
    .join(", ")
  return (
    <span role="img" aria-label={`Activity: ${summary || "no repositories"}`} className={cn("flex flex-wrap gap-0.5", className)}>
      {repos.map((r) => (
        <span key={r.address} className={`lvl-${activity(r, now)} cell`} title={r.name} />
      ))}
    </span>
  )
}

/** ActivityLegend explains the activity colours. */
export function ActivityLegend({ pulse }: { pulse?: Pulse }) {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Activity legend">
      {activities.map((a) => (
        <li key={a.level} className={`lvl-${a.level} flex items-center gap-1.5`} title={a.within}>
          <span aria-hidden="true" className="cell" />
          {a.label}
          {pulse && <span className="font-mono text-foreground">{count(pulse.levels[a.level])}</span>}
        </li>
      ))}
    </ul>
  )
}
