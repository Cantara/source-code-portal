"use client"

import type { Repo } from "@/lib/api/client"
import { ago, count } from "@/lib/format"
import { activities, activity, lastChange, type Activity, type Pulse } from "./health"
import { issuesUrl, licence } from "./licence"
import { cn } from "@/lib/cn"
import { levels } from "@/entities/health/health"
import { Hint } from "@/shared/hint"

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
    <Hint
      heading={label[level].label}
      swatch={`lvl-${level}`}
      meta={`${ago(lastChange(repo), now)} ago`}
      render={(t) => (
        <span {...t} className={cn(`lvl-${level} led led-live cursor-help`, className)} aria-hidden={decorative || undefined}>
          {!decorative && <span className="sr-only">{label[level].label}</span>}
        </span>
      )}
    />
  )
}

/**
 * HealthBadges are a repository's health at a glance: its score when it is not good (the findings in its title), activity and age,
 * latest release, licence, open issues and language. Every badge carries its meaning in text, so colour is never the only signal.
 */
export function HealthBadges({ repo, now, className }: { repo: Repo; now: number; className?: string }) {
  const level = activity(repo, now)
  const lic = licence(repo.license)
  const health = repo.health
  return (
    <span className={cn("flex flex-wrap items-center gap-1", className)}>
      {health && health.level !== "good" && (
        <Hint
          heading={`Health ${health.score}`}
          rows={health.findings.map((f) => ({
            swatch: levels[health.level].className,
            label: f.message,
            value: `−${f.penalty}`,
          }))}
          swatch={levels[health.level].className}
          meta={levels[health.level].label}
          render={(t) => (
            <span {...t} className={cn(levels[health.level].className, "chip cursor-help")}>
              <span className="sr-only">Health </span>
              {health.score} {levels[health.level].label.toLowerCase()}
              <span className="sr-only">: {health.findings.map((f) => f.message).join("; ")}</span>
            </span>
          )}
        />
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
      />
      {repo.release ? (
        <Hint
          heading={`Release ${repo.release.tag}`}
          meta={`${ago(repo.release.publishedAt, now)} ago`}
          swatch="lvl-recent"
          render={(t) => (
            <a {...t} href={repo.release?.url} rel="noreferrer nofollow" className="lvl-recent chip max-w-40 hover:underline">
              <span className="sr-only">Release</span>
              <span className="truncate">{repo.release?.tag}</span>
            </a>
          )}
        />
      ) : (
        <Hint
          heading="No release"
          swatch="lvl-dormant"
          render={(t) => (
            <span {...t} className="lvl-dormant chip cursor-help">
              no release
            </span>
          )}
        />
      )}
      {lic ? (
        <Hint
          heading={`Licence ${lic.spdx}`}
          meta="its text at SPDX"
          swatch="lvl-dormant"
          render={(t) => (
            <a
              {...t}
              href={lic.url}
              rel="noreferrer nofollow"
              aria-label={`${lic.short} licence, ${lic.spdx}`}
              className="lvl-dormant chip hover:underline"
            >
              {lic.short}
            </a>
          )}
        />
      ) : (
        <Hint
          heading="No licence"
          swatch="lvl-danger"
          render={(t) => (
            <span {...t} className="lvl-danger chip cursor-help">
              no licence
            </span>
          )}
        />
      )}
      <Hint
        heading={`${count(repo.openIssues)} open ${repo.openIssues === 1 ? "issue or pull request" : "issues and pull requests"}`}
        meta="on GitHub"
        swatch={repo.openIssues > 20 ? "lvl-quiet" : "lvl-dormant"}
        render={(t) => (
          <a
            {...t}
            href={issuesUrl(repo.htmlUrl)}
            rel="noreferrer"
            aria-label={`${count(repo.openIssues)} ${repo.openIssues === 1 ? "issue" : "issues"} open on GitHub`}
            className={cn("chip hover:underline", repo.openIssues > 20 ? "lvl-quiet" : "lvl-dormant")}
          >
            {count(repo.openIssues)} {repo.openIssues === 1 ? "issue" : "issues"}
          </a>
        )}
      />
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
            meta={`${label[level].label} · ${ago(lastChange(r), now)} ago`}
            render={(t) => <span {...t} className={`lvl-${level} cell cursor-help`} />}
          />
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
        />
      ))}
    </ul>
  )
}
