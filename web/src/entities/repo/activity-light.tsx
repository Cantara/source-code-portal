"use client"

import type { Repo } from "@/lib/api/client"
import { ago } from "@/lib/format"
import { activityRule } from "@/lib/rules"
import { useRules } from "@/lib/rules-context"
import { activity, lastChange } from "./health"
import { cn } from "@/lib/cn"
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
