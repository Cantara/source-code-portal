import type { ComponentType } from "react"
import { ArchiveIcon, FlagIcon, HourglassIcon, InboxIcon, PullIcon, ScaleIcon, SyncOffIcon, TagIcon } from "@/components/ui/icons"
import { cn } from "@/lib/cn"
import type { Severity } from "@/lib/rules"

// The health's drawings without behaviour: the cards (findings.tsx, with hints) and the guide's legend (a server page without
// client JavaScript) draw them alike.

/**
 * kindIcons are the drawings of the kinds of finding the portal knows; their names, why they matter and what they cost come from
 * the server's rules (lib/rules.ts), and a kind with no drawing yet shows the flag.
 */
const kindIcons: Record<string, ComponentType<{ className?: string }>> = {
  failing: SyncOffIcon,
  dormant: ArchiveIcon,
  quiet: HourglassIcon,
  unreleased: TagIcon,
  "no-licence": ScaleIcon,
  issues: InboxIcon,
  reviews: PullIcon,
  updates: PullIcon,
}

/** severities are the severities' colours; their names and where they start come from the rules. */
export const severities: Record<Severity, string> = { high: "sev-high", medium: "sev-medium", low: "sev-low" }

/** FindingIcon is a finding's kind as an icon on a tile of its severity's colour, so the shape names it and the colour weighs it. */
export function FindingIcon({ code, severity, className }: { code: string; severity: Severity; className?: string }) {
  const Icon = kindIcons[code] ?? FlagIcon
  return (
    <span aria-hidden="true" className={cn("finding-icon", severities[severity], className)}>
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
