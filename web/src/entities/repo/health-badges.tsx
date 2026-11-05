"use client"

import type { Repo } from "@/lib/api/client"
import { ago, count } from "@/lib/format"
import { activityRule, levelLabel, span } from "@/lib/rules"
import { useRules } from "@/lib/rules-context"
import { activity, lastChange } from "./health"
import { issuesUrl, licence } from "./licence"
import { ecosystemLabel, newestPackage } from "./packages"
import { cn } from "@/lib/cn"
import { levels } from "@/entities/health/health"
import { Hint } from "@/shared/hint"

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
  const rules = useRules()
  const level = activity(repo, now, rules)
  const name = activityRule(level, rules).label
  return (
    <Hint
      heading={name}
      swatch={`lvl-${level}`}
      meta={`${ago(lastChange(repo), now)} ago`}
      render={(t) => (
        <span {...t} className={cn(`lvl-${level} led led-live cursor-help`, className)} aria-hidden={decorative || undefined}>
          {!decorative && <span className="sr-only">{name}</span>}
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
  const rules = useRules()
  const level = activity(repo, now, rules)
  const rule = activityRule(level, rules)
  const lic = licence(repo.license)
  const pkg = newestPackage(repo)
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
          meta={levelLabel(health.level, rules)}
          render={(t) => (
            <span {...t} className={cn(levels[health.level].className, "chip cursor-help")}>
              <span className="sr-only">Health </span>
              {health.score} {levelLabel(health.level, rules).toLowerCase()}
              <span className="sr-only">: {health.findings.map((f) => f.message).join("; ")}</span>
            </span>
          )}
        />
      )}
      <Hint
        heading={rule.label}
        swatch={`lvl-${level}`}
        meta={span(level, rules)}
        children={rule.about || undefined}
        render={(t) => (
          <span {...t} className={`lvl-${level} chip cursor-help`}>
            <span aria-hidden="true" className="led size-1.5" />
            <span className="sr-only">{rule.label}, last change</span>
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
      ) : pkg ? (
        <Hint
          heading={`${ecosystemLabel(pkg.ecosystem)} ${pkg.version} at ${pkg.registry}`}
          meta={pkg.publishedAt ? `${ago(pkg.publishedAt, now)} ago` : undefined}
          swatch="lvl-recent"
          rows={repo.packages.map((p) => ({ swatch: "lvl-recent", label: p.name, value: p.version }))}
          render={(t) =>
            pkg.url ? (
              <a
                {...t}
                href={pkg.url}
                rel="noreferrer nofollow"
                aria-label={`${pkg.version} · ${ecosystemLabel(pkg.ecosystem)}, published package ${pkg.name}`}
                className="lvl-recent chip max-w-48 hover:underline"
              >
                <span className="truncate">
                  {pkg.version} · {ecosystemLabel(pkg.ecosystem)}
                </span>
              </a>
            ) : (
              <span {...t} className="lvl-recent chip max-w-48 cursor-help">
                <span className="truncate">
                  {pkg.version} · {ecosystemLabel(pkg.ecosystem)}
                </span>
              </span>
            )
          }
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
