import Link from "next/link"
import { useMemo, type Ref } from "react"
import type { Commit } from "@/lib/api/client"
import { addressPath } from "@/lib/paths"
import { InitialsAvatar } from "@/shared/initials-avatar"
import { RelativeTime } from "@/shared/relative-time"
import { EmptyState } from "@/shared/empty-state"
import { graph as layout, maxLanes, type Edge, type GraphRow } from "./graph"

/**
 * CommitList is commits newest first: subject, repository, author and time. With graph (the commits of one repository) each row
 * draws its part of the git graph, and the subject keeps to one line.
 */
export function CommitList({
  commits,
  showRepo = true,
  graph = false,
  firstRef,
}: {
  commits: Commit[]
  showRepo?: boolean
  graph?: boolean
  firstRef?: Ref<HTMLLIElement>
}) {
  const g = useMemo(() => (graph ? layout(commits) : undefined), [graph, commits])
  if (commits.length === 0) return <EmptyState title="No commits yet" />
  return (
    <ol className={g ? undefined : "divide-y divide-border"}>
      {commits.map((c, i) => {
        const row = g?.rows[i]
        return (
          <li
            key={c.repo + c.sha}
            ref={i === 0 ? firstRef : undefined}
            tabIndex={i === 0 ? -1 : undefined}
            className={row ? "flex h-13 items-start gap-2 focus:outline-none" : "flex gap-3 py-2 focus:outline-none"}
          >
            {row && g && <GraphCell row={row} lanes={g.lanes} />}
            <span className={row ? "mt-0.5" : undefined}>
              <InitialsAvatar name={c.authorName || c.authorLogin} />
            </span>
            <div className={row ? "min-w-0 flex-1 py-1" : "min-w-0 flex-1"}>
              <p className={row ? "truncate leading-6" : "break-words"} title={row ? c.subject : undefined}>
                {c.url ? (
                  <a href={c.url} className="hover:underline" rel="noreferrer">
                    {c.subject}
                  </a>
                ) : (
                  c.subject
                )}
              </p>
              <p className={row ? "truncate text-sm text-muted-foreground" : "text-sm text-muted-foreground"}>
                {showRepo && (
                  <>
                    <Link href={addressPath(c.repo)} className="font-medium hover:underline">
                      {c.repo}
                    </Link>{" "}
                    ·{" "}
                  </>
                )}
                {c.authorName || c.authorLogin} · <RelativeTime at={c.committedAt} /> ·{" "}
                <span className="font-mono">{c.sha.slice(0, 7)}</span>
              </p>
            </div>
          </li>
        )
      })}
    </ol>
  )
}

// The row's graph in viewBox units: lanes `unit` apart, the row `rowHeight` high (h-13), the node on the subject line.
const unit = 16
const rowHeight = 52
const nodeY = 16
const ring = 4 // the merge ring's radius; lines stop at its edge

const x = (lane: number) => unit / 2 + unit * Math.min(lane, maxLanes - 1)

function line(e: Edge, y1: number, y2: number, key: string) {
  const [x1, x2] = [x(e.from), x(e.to)]
  const mid = (y1 + y2) / 2
  const d = x1 === x2 ? `M${x1} ${y1}V${y2}` : `M${x1} ${y1}C${x1} ${mid} ${x2} ${mid} ${x2} ${y2}`
  return (
    <path
      key={key}
      d={d}
      className={`lane-${e.color}`}
      stroke="currentColor"
      strokeWidth={2}
      fill="none"
      strokeDasharray={e.open ? "3 3" : undefined}
    />
  )
}

/** GraphCell is one row of the git graph: lines in from above, the node (a ring for a merge), lines out below. */
function GraphCell({ row, lanes }: { row: GraphRow; lanes: number }) {
  const cx = x(row.lane)
  const gap = row.merge ? ring : 0 // lines meet a ring at its edge, so nothing shows through it
  return (
    <svg aria-hidden="true" viewBox={`0 0 ${unit * lanes} ${rowHeight}`} className="h-13 w-auto shrink-0">
      {row.above.map((e, i) => line(e, 0, e.to === row.lane ? nodeY - gap : nodeY, `a${i}`))}
      {row.below.map((e, i) => line(e, e.from === row.lane ? nodeY + gap : nodeY, rowHeight, `b${i}`))}
      {row.merge ? (
        <circle
          cx={cx}
          cy={nodeY}
          r={ring - 1}
          className={`lane-${row.color}`}
          stroke="currentColor"
          strokeWidth={2}
          fill="none"
        />
      ) : (
        <circle cx={cx} cy={nodeY} r={4} className={`lane-${row.color}`} fill="currentColor" />
      )}
    </svg>
  )
}
