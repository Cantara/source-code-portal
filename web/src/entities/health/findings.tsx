"use client"

import Link from "next/link"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { addressPath } from "@/lib/paths"
import { Hint } from "@/shared/hint"
import { levels } from "./health"
import { kindOf, levelLabel, levelRanges, severityLabel, severityOf } from "@/lib/rules"
import { useRules } from "@/lib/rules-context"
import { FindingIcon, ScoreRing, severities } from "./kinds"

type Level = Schemas["HealthLevel"]
type Finding = Schemas["Finding"] | Schemas["RepoFinding"]

/** findingPath is where a repository's finding is told, with its next step: its row on the repository's page. */
export const findingPath = (repo: string, code: string) => `${addressPath(repo)}#finding-${code}`

/**
 * FindingMark is a finding as its kind's icon on its severity's tile, with its whole story in a hint the moment it is pointed at,
 * focused or tapped: the kind, its severity and cost, what was found and why it matters. Given its repository it is a link to the
 * finding on the repository's page, where its next step is (a tap shows the hint first, a second tap follows it, as the fleet's
 * tiles do), so a glance at a card can start the fix.
 */
export function FindingMark({ finding: f, repo, className }: { finding: Finding; repo?: string; className?: string }) {
  const rules = useRules()
  const kind = kindOf(f.code, rules)
  const sev = severityOf(f.penalty, rules)
  const sevLabel = severityLabel(sev, rules)
  const at = repo ?? ("repo" in f ? f.repo : undefined)
  return (
    <Hint
      heading={kind.label}
      swatch={severities[sev]}
      meta={`${sevLabel} · −${f.penalty}`}
      rows={[
        ...(at ? [{ label: at.split("/")[1] ?? at, value: kind.action.label || undefined }] : []),
        { swatch: severities[sev], label: f.message },
      ]}
      render={(t) =>
        at ? (
          <Link
            {...t}
            href={findingPath(at, f.code)}
            aria-label={`${kind.label} in ${at.split("/")[1] ?? at}, ${sevLabel.toLowerCase()}: ${f.message}`}
            className={cn("shrink-0 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring", className)}
          >
            <FindingIcon code={f.code} severity={sev} />
          </Link>
        ) : (
          <span {...t} className={cn("shrink-0 cursor-help", className)}>
            <FindingIcon code={f.code} severity={sev} />
          </span>
        )
      }
    >
      {kind.about || undefined}
    </Hint>
  )
}

/**
 * FindingList is what costs a score points, the most costly first: each finding its mark, then what it is; product findings name
 * and link their repository. On a repository's own page (github: its GitHub page) each row is the target the marks link to and
 * ends with the finding's next step on GitHub. The points are in the hint, not in the row.
 */
export function FindingList({ findings, github, className }: { findings: Finding[]; github?: string; className?: string }) {
  const rules = useRules()
  return (
    <ul className={cn("flex flex-col gap-1.5 text-sm", className)}>
      {findings.map((f, i) => {
        const kind = kindOf(f.code, rules)
        const sevLabel = severityLabel(severityOf(f.penalty, rules), rules)
        return (
          <li
            key={i}
            id={github ? `finding-${f.code}` : undefined}
            className={cn(
              "flex items-start gap-2",
              github && "scroll-mt-24 rounded-sm target:bg-accent target:ring-4 target:ring-accent",
            )}
          >
            <span className="mt-px">
              <FindingMark finding={f} />
            </span>
            <span className="min-w-0">
              <span className="sr-only">
                {kind.label}, {sevLabel.toLowerCase()}, costs {f.penalty} points:{" "}
              </span>
              {"repo" in f && (
                <>
                  <Link href={findingPath(f.repo, f.code)} className="font-medium hover:underline">
                    {f.repo.split("/")[1]}
                  </Link>
                  {": "}
                </>
              )}
              {f.message}
              {github && kind.action.label && (
                <>
                  {" "}
                  <a
                    href={kind.action.path ? `${github}/${kind.action.path}` : github}
                    rel="noreferrer"
                    className="whitespace-nowrap text-primary hover:underline"
                  >
                    {kind.action.label} ↗
                  </a>
                </>
              )}
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
  const rules = useRules()
  const l = { ...levels[health.level], label: levelLabel(health.level, rules) }
  return (
    <Hint
      heading={`Health ${health.score}`}
      swatch={l.className}
      meta={l.label}
      rows={levelRanges(rules).map((r) => ({ swatch: levels[r.level].className, label: r.label, value: r.range }))}
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
