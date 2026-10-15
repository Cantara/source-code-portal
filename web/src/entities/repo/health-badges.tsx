import type { Repo } from "@/lib/api/client"
import { ago, count, exactUTC } from "@/lib/format"
import { activities, activity, lastChange, type Activity, type Pulse } from "./health"
import { cn } from "@/lib/cn"
import { levels } from "@/entities/health/health"

const label = Object.fromEntries(activities.map((a) => [a.level, a])) as Record<Activity, (typeof activities)[number]>

// The components take the view's instant (useNow, read once per view): one clock subscription per page, not one per repository.

/**
 * ActivityLight is a repository's activity as a coloured light with its label for screen readers (none when decorative: the
 * health badges beside it say it).
 */
export function ActivityLight({
  repo,
  now,
  decorative = false,
  className,
}: {
  repo: Repo
  now: number
  decorative?: boolean
  className?: string
}) {
  const level = activity(repo, now)
  return (
    <span
      className={cn(`lvl-${level} led led-live`, className)}
      title={`${label[level].label}: ${label[level].within}`}
      aria-hidden={decorative || undefined}
    >
      {!decorative && <span className="sr-only">{label[level].label}</span>}
    </span>
  )
}

/**
 * HealthBadges are a repository's health at a glance: its score when it is not good (the findings in its title), activity and age,
 * latest release, licence, open issues and language. Every badge carries its meaning in text, so colour is never the only signal.
 */
export function HealthBadges({ repo, now, className }: { repo: Repo; now: number; className?: string }) {
  const level = activity(repo, now)
  const licensed = repo.license !== "" && repo.license !== "NOASSERTION"
  return (
    <span className={cn("flex flex-wrap items-center gap-1", className)}>
      {repo.health && repo.health.level !== "good" && (
        <span
          className={cn(levels[repo.health.level].className, "chip")}
          title={repo.health.findings.map((f) => `−${f.penalty} ${f.message}`).join("\n")}
        >
          <span className="sr-only">Health </span>
          {repo.health.score} {levels[repo.health.level].label.toLowerCase()}
          <span className="sr-only">: {repo.health.findings.map((f) => f.message).join("; ")}</span>
        </span>
      )}
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
export function HealthStrip({ repos, pulse, now, className }: { repos: Repo[]; pulse: Pulse; now: number; className?: string }) {
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
