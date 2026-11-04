"use client"

import Link from "next/link"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { addressPath } from "@/lib/paths"
import { Hint } from "@/shared/hint"
import { levels } from "./health"
import { FindingIcon, findingKinds, ScoreRing, severities, severity } from "./kinds"

type Level = Schemas["HealthLevel"]
type Finding = Schemas["Finding"] | Schemas["RepoFinding"]

/**
 * FindingList is what costs a score points, the most costly first: each finding its kind's icon on its severity's tile, then what
 * it is and what to do; product findings name and link their repository. The points are in the hint, not in the row.
 */
export function FindingList({ findings, className }: { findings: Finding[]; className?: string }) {
  return (
    <ul className={cn("flex flex-col gap-1.5 text-sm", className)}>
      {findings.map((f, i) => {
        const kind = findingKinds[f.code] ?? findingKinds.quiet
        const sev = severities[severity(f.penalty)]
        return (
          <li key={i} className="flex items-start gap-2">
            <Hint
              heading={kind.label}
              swatch={sev.className}
              meta={`${sev.label}`}
              rows={[{ swatch: sev.className, label: "Costs", value: `${f.penalty} of 100 points` }]}
              children={kind.about}
              render={(t) => (
                <span {...t} className="mt-px shrink-0">
                  <FindingIcon code={f.code} penalty={f.penalty} />
                </span>
              )}
            />
            <span className="min-w-0">
              <span className="sr-only">
                {kind.label}, {sev.label.toLowerCase()}, costs {f.penalty} points:{" "}
              </span>
              {"repo" in f && (
                <>
                  <Link href={addressPath(f.repo)} className="font-medium hover:underline">
                    {f.repo.split("/")[1]}
                  </Link>
                  {": "}
                </>
              )}
              {f.message}
            </span>
          </li>
        )
      })}
    </ul>
  )
}

/**
 * HealthScore is a score with its level: a ring of the level's colour with the number, and the level in words (on a card only when
 * it is not good, so a healthy card stays quiet). Its hint gives the levels.
 */
export function HealthScore({
  health,
  large = false,
  quiet = false,
  className,
}: {
  health: { score: number; level: Level }
  large?: boolean
  /** The level's word only when it is not good. */
  quiet?: boolean
  className?: string
}) {
  const l = levels[health.level]
  return (
    <Hint
      heading={`Health ${health.score}`}
      swatch={l.className}
      meta={l.label}
      rows={[
        { swatch: levels.good.className, label: levels.good.label, value: "80–100" },
        { swatch: levels.watch.className, label: levels.watch.label, value: "50–79" },
        { swatch: levels.attention.className, label: levels.attention.label, value: "0–49" },
      ]}
      render={(t) => (
        <span {...t} className={cn(l.className, "inline-flex items-center gap-2", className)}>
          <span className="sr-only">Health </span>
          <ScoreRing score={health.score} large={large} />
          {quiet && health.level === "good" ? (
            <span className="sr-only">{l.label}</span>
          ) : (
            <span className="chip">{l.label}</span>
          )}
        </span>
      )}
    />
  )
}
