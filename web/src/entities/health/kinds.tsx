import type { ComponentType } from "react"
import { ArchiveIcon, HourglassIcon, InboxIcon, ScaleIcon, SyncOffIcon, TagIcon } from "@/components/ui/icons"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"

// The health's drawings without behaviour: the cards (findings.tsx, with hints) and the guide's legend (a server page without
// client JavaScript) draw them alike.

type Code = Schemas["Finding"]["code"]

/**
 * findingKinds are the kinds of finding (internal/health Assess), each with its icon, its name in a word or two and why it matters
 * in one sentence: the hint and the guide's legend say it.
 */
export const findingKinds: Record<Code, { Icon: ComponentType<{ className?: string }>; label: string; about: string }> = {
  failing: {
    Icon: SyncOffIcon,
    label: "Not updating",
    about: "The portal cannot read it from GitHub, so what it shows may be old.",
  },
  dormant: {
    Icon: ArchiveIcon,
    label: "Dormant",
    about: "No one has worked on it for two years: archive it, or bring it back to life.",
  },
  quiet: { Icon: HourglassIcon, label: "Quiet", about: "No one has changed it for six months: is it done, or forgotten?" },
  unreleased: {
    Icon: TagIcon,
    label: "Unreleased",
    about: "Changes since its last release, three months or more ago, wait to be released.",
  },
  "no-licence": {
    Icon: ScaleIcon,
    label: "No licence",
    about: "A public repository without a licence cannot safely be used by others.",
  },
  issues: { Icon: InboxIcon, label: "Backlog", about: "Many open issues and pull requests wait for an answer." },
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

/** ScoreRing is a score as a ring filled to it in its level's colour, the number inside. */
export function ScoreRing({ score, large }: { score: number; large: boolean }) {
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
