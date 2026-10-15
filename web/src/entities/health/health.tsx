import Link from "next/link"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { addressPath } from "@/lib/paths"

type Level = Schemas["HealthLevel"]
type Finding = Schemas["Finding"] | Schemas["RepoFinding"]

/** levels are the health levels with their colour class and label. */
export const levels: Record<Level, { className: string; label: string }> = {
  good: { className: "lvl-active", label: "Good" },
  watch: { className: "lvl-quiet", label: "Watch" },
  attention: { className: "lvl-danger", label: "Needs attention" },
}

/** HealthScore is a score with its level: a light, the number and the level in words, so colour is never the only signal. */
export function HealthScore({
  health,
  large = false,
  className,
}: {
  health: { score: number; level: Level }
  large?: boolean
  className?: string
}) {
  const l = levels[health.level]
  return (
    <span className={cn(l.className, "inline-flex items-center gap-1.5", className)}>
      <span aria-hidden="true" className="led" />
      <span className={cn("font-mono font-semibold tabular-nums", large ? "text-2xl" : "text-sm")}>{health.score}</span>
      <span className="chip">{l.label}</span>
    </span>
  )
}

/** FindingList is what costs a score points, the most costly first; product findings name and link their repository. */
export function FindingList({ findings, className }: { findings: Finding[]; className?: string }) {
  return (
    <ul className={cn("flex flex-col gap-1 text-sm", className)}>
      {findings.map((f, i) => (
        <li key={i} className="flex items-baseline gap-2">
          <span className="w-8 shrink-0 text-right font-mono text-xs text-muted-foreground tabular-nums">−{f.penalty}</span>
          <span className="min-w-0">
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
      ))}
    </ul>
  )
}
