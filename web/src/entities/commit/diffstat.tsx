import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { count, plural } from "@/lib/format"
import { Hint } from "@/shared/hint"

export type Stats = Schemas["CommitStats"]

/** blocks shares five blocks out as GitHub's diffstat does: the added share, the removed share, the rest neutral. */
export function blocks(s: Stats): ("add" | "del" | "none")[] {
  const total = s.additions + s.deletions
  if (total === 0) return Array<"none">(5).fill("none")
  const add = Math.round((5 * s.additions) / total)
  const del = Math.min(5 - add, Math.round((5 * s.deletions) / total))
  return [...Array<"add">(add).fill("add"), ...Array<"del">(del).fill("del"), ...Array<"none">(5 - add - del).fill("none")]
}

/** said is the stats in words: "12 additions and 3 deletions in 2 files". */
export function said(s: Stats): string {
  const files = s.files === undefined ? "" : ` in ${plural(s.files, "file", "files")}`
  return `${plural(s.additions, "addition", "additions")} and ${plural(s.deletions, "deletion", "deletions")}${files}`
}

/**
 * DiffStat is what a commit changed (design/diffstat.md): `+12 −3` in the diff colours and five blocks sharing the change out, with
 * the same in words in a hint and for screen readers. Nothing for a commit without stats.
 */
export function DiffStat({ stats, bar = true, className }: { stats?: Stats; bar?: boolean; className?: string }) {
  if (!stats) return null
  const words = said(stats)
  return (
    <Hint
      heading={words}
      render={(t) => (
        <span
          {...t}
          className={cn("inline-flex shrink-0 cursor-help items-center gap-1 font-mono text-xs tabular-nums", className)}
        >
          <span className="sr-only">{words}</span>
          <span aria-hidden="true" className="text-[var(--diff-add)]">
            +{count(stats.additions)}
          </span>
          <span aria-hidden="true" className="text-[var(--diff-del)]">
            −{count(stats.deletions)}
          </span>
          {bar && (
            <span aria-hidden="true" className="ml-0.5 inline-flex gap-px">
              {blocks(stats).map((b, i) => (
                <span
                  key={i}
                  className={cn(
                    "size-2 rounded-[1px]",
                    b === "add" ? "bg-[var(--diff-add)]" : b === "del" ? "bg-[var(--diff-del)]" : "bg-muted-foreground/30",
                  )}
                />
              ))}
            </span>
          )}
        </span>
      )}
    />
  )
}
