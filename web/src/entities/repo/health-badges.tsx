"use client"

import type { Repo } from "@/lib/api/client"
import { ago, count, exactUTC } from "@/lib/format"
import { activities, activity, lastChange, type Activity, type Pulse } from "./health"
import { cn } from "@/lib/cn"
import { levels } from "@/entities/health/health"
import { Hint } from "@/shared/hint"

const label = Object.fromEntries(activities.map((a) => [a.level, a])) as Record<Activity, (typeof activities)[number]>

/** within says what an activity level means, as a sentence's start: "Recent means a commit by a person in the last 6 months". */
function within(level: Activity): string {
  return `${label[level].label} means ${label[level].within}`
}

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
    <Hint
      heading={label[level].label}
      swatch={`lvl-${level}`}
      meta={label[level].span}
      render={(t) => (
        <span {...t} className={cn(`lvl-${level} led led-live cursor-help`, className)} aria-hidden={decorative || undefined}>
          {!decorative && <span className="sr-only">{label[level].label}</span>}
        </span>
      )}
    >
      Last change {ago(lastChange(repo), now)} ago. {within(level)}.
    </Hint>
  )
}

/**
 * HealthBadges are a repository's health at a glance: its score when it is not good (the findings in its title), activity and age,
 * latest release, licence, open issues and language. Every badge carries its meaning in text, so colour is never the only signal.
 */
export function HealthBadges({ repo, now, className }: { repo: Repo; now: number; className?: string }) {
  const level = activity(repo, now)
  const licensed = repo.license !== "" && repo.license !== "NOASSERTION"
  const health = repo.health
  return (
    <span className={cn("flex flex-wrap items-center gap-1", className)}>
      {health && health.level !== "good" && (
        <Hint
          heading={`Health ${health.score} of 100`}
          swatch={levels[health.level].className}
          meta={levels[health.level].label}
          render={(t) => (
            <span {...t} className={cn(levels[health.level].className, "chip cursor-help")}>
              <span className="sr-only">Health </span>
              {health.score} {levels[health.level].label.toLowerCase()}
              <span className="sr-only">: {health.findings.map((f) => f.message).join("; ")}</span>
            </span>
          )}
        >
          <ul className="flex flex-col gap-0.5">
            {health.findings.map((f) => (
              <li key={f.message} className="flex gap-2">
                <span className="w-7 shrink-0 text-right font-mono text-foreground">−{f.penalty}</span>
                <span>{f.message}</span>
              </li>
            ))}
          </ul>
        </Hint>
      )}
      <Hint
        heading={label[level].label}
        swatch={`lvl-${level}`}
        meta={label[level].span}
        render={(t) => (
          <span {...t} className={`lvl-${level} chip cursor-help`}>
            <span aria-hidden="true" className="led size-1.5" />
            <span className="sr-only">{label[level].label}, last change</span>
            <time dateTime={lastChange(repo)} suppressHydrationWarning>
              {ago(lastChange(repo), now)}
            </time>
          </span>
        )}
      >
        {within(level)}.<span className="block font-mono text-[0.6875rem]">Last change {exactUTC(lastChange(repo))}</span>
      </Hint>
      {repo.release ? (
        <Hint
          heading={`Latest release ${repo.release.tag}`}
          swatch="lvl-recent"
          render={(t) => (
            <a {...t} href={repo.release?.url} rel="noreferrer nofollow" className="lvl-recent chip max-w-40 hover:underline">
              <span className="sr-only">Release</span>
              <span className="truncate">{repo.release?.tag}</span>
            </a>
          )}
        >
          Published {exactUTC(repo.release.publishedAt)}. Select to open it on GitHub.
        </Hint>
      ) : (
        <Hint
          heading="No release"
          swatch="lvl-dormant"
          render={(t) => (
            <span {...t} className="lvl-dormant chip cursor-help">
              no release
            </span>
          )}
        >
          No GitHub release is published for this repository.
        </Hint>
      )}
      <Hint
        heading={licensed ? `Licence ${repo.license}` : "No licence"}
        swatch={licensed ? "lvl-dormant" : "lvl-danger"}
        render={(t) => (
          <span {...t} className={cn("chip cursor-help", licensed ? "lvl-dormant" : "lvl-danger")}>
            {licensed ? repo.license : "no licence"}
          </span>
        )}
      >
        {licensed
          ? "The licence GitHub detects in the repository."
          : "GitHub detects no licence: others may not use the code. It costs a public repository health points."}
      </Hint>
      <Hint
        heading={`${count(repo.openIssues)} open`}
        swatch={repo.openIssues > 20 ? "lvl-quiet" : "lvl-dormant"}
        render={(t) => (
          <span {...t} className={cn("chip cursor-help", repo.openIssues > 20 ? "lvl-quiet" : "lvl-dormant")}>
            {count(repo.openIssues)}
            <span className="sr-only"> open</span> issues
          </span>
        )}
      >
        Open issues and pull requests on GitHub. More than 20 is marked, and costs health points.
      </Hint>
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
      {repos.map((r) => {
        const level = activity(r, now)
        return (
          <Hint
            key={r.address}
            heading={r.name}
            swatch={`lvl-${level}`}
            meta={label[level].span ? `${label[level].label} · ${label[level].span}` : label[level].label}
            render={(t) => <span {...t} className={`lvl-${level} cell cursor-help`} />}
          >
            Last change {ago(lastChange(r), now)} ago. {within(level)}.
          </Hint>
        )
      })}
    </span>
  )
}

/** ActivityLegend explains the activity colours. */
export function ActivityLegend({ pulse }: { pulse?: Pulse }) {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Activity legend">
      {activities.map((a) => (
        <Hint
          key={a.level}
          heading={a.label}
          swatch={`lvl-${a.level}`}
          meta={a.span}
          render={(t) => (
            <li
              {...t}
              tabIndex={0}
              className={`lvl-${a.level} flex cursor-help items-center gap-1.5 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring`}
            >
              <span aria-hidden="true" className="cell" />
              {a.label}
              {pulse && <span className="font-mono text-foreground">{count(pulse.levels[a.level])}</span>}
            </li>
          )}
        >
          {within(a.level)}.
        </Hint>
      ))}
    </ul>
  )
}
