"use client"

import Link from "next/link"
import type { ComponentType } from "react"
import { ArchiveIcon, HourglassIcon, InboxIcon, ScaleIcon, SyncOffIcon, TagIcon } from "@/components/ui/icons"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { addressPath } from "@/lib/paths"
import { Hint } from "@/shared/hint"
import { levels } from "./health"

type Level = Schemas["HealthLevel"]
type Finding = Schemas["Finding"] | Schemas["RepoFinding"]
type Code = Schemas["Finding"]["code"]

/** findingKinds are the kinds of finding (internal/health Assess), each with its icon and its name in a word or two. */
export const findingKinds: Record<Code, { Icon: ComponentType<{ className?: string }>; label: string }> = {
  failing: { Icon: SyncOffIcon, label: "Not updating" },
  dormant: { Icon: ArchiveIcon, label: "Dormant" },
  quiet: { Icon: HourglassIcon, label: "Quiet" },
  unreleased: { Icon: TagIcon, label: "Unreleased" },
  "no-licence": { Icon: ScaleIcon, label: "No licence" },
  issues: { Icon: InboxIcon, label: "Backlog" },
}

/** Severity is how much a finding weighs: high from 25 points, medium from 20, low below. */
export type Severity = "high" | "medium" | "low"

export const severities: Record<Severity, { className: string; label: string }> = {
  high: { className: "sev-high", label: "High" },
  medium: { className: "sev-medium", label: "Medium" },
  low: { className: "sev-low", label: "Low" },
}

export function severity(penalty: number): Severity {
  return penalty >= 25 ? "high" : penalty >= 20 ? "medium" : "low"
}

/** FindingIcon is a finding's kind as an icon on a tile of its severity's colour, so the shape names it and the colour weighs it. */
export function FindingIcon({ code, penalty, className }: { code: Code; penalty: number; className?: string }) {
  const { Icon } = findingKinds[code] ?? findingKinds.quiet
  return (
    <span aria-hidden="true" className={cn("finding-icon", severities[severity(penalty)].className, className)}>
      <Icon className="size-3.5" />
    </span>
  )
}

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

/** ScoreRing is a score as a ring filled to it in its level's colour, the number inside. */
function ScoreRing({ score, large }: { score: number; large: boolean }) {
  return (
    <span className={cn("relative grid shrink-0 place-items-center", large ? "size-12" : "size-8")}>
      <svg viewBox="0 0 36 36" aria-hidden="true" className="absolute inset-0 -rotate-90">
        <circle cx="18" cy="18" r="15.5" fill="none" strokeWidth="3" className="stroke-border" />
        <circle
          cx="18"
          cy="18"
          r="15.5"
          fill="none"
          strokeWidth="3"
          strokeLinecap="round"
          pathLength={100}
          strokeDasharray={`${Math.max(0, Math.min(100, score))} 100`}
          className="score-arc"
        />
      </svg>
      <span className={cn("font-mono font-semibold tabular-nums", large ? "text-base" : "text-[0.6875rem]")}>{score}</span>
    </span>
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
