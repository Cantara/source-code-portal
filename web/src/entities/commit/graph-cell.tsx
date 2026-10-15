import { maxLanes, type Edge, type GraphRow } from "./graph"

// The graph in viewBox units: lanes `unit` apart, the node `nodeY` from the row's top.
const unit = 16
const ring = 4 // the merge ring's radius; lines stop at its edge

const x = (lane: number) => unit / 2 + unit * Math.min(lane, maxLanes - 1)

function line(e: Edge, y1: number, y2: number, key: string, faded: boolean) {
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
      opacity={faded ? 0.3 : undefined}
    />
  )
}

/**
 * GraphCell is one row of the git graph, `height` high with the node `nodeY` from its top: lines in from above, the node (a ring for
 * a merge), lines out below. With `lane` (a selected commit's lane and colour) the lines of other lanes fade.
 */
export function GraphCell({
  row,
  lanes,
  height,
  nodeY,
  className,
  lane,
}: {
  row: GraphRow
  lanes: number
  height: number
  nodeY: number
  /** the height class matching height (no style attribute: the CSP allows none) */
  className: string
  lane?: { lane: number; color: number }
}) {
  const cx = x(row.lane)
  const gap = row.merge ? ring : 0 // lines meet a ring at its edge, so nothing shows through it
  const faded = (e: Edge) =>
    lane !== undefined &&
    (e.color !== lane.color || (Math.min(e.from, maxLanes - 1) !== lane.lane && Math.min(e.to, maxLanes - 1) !== lane.lane))
  const nodeFaded = lane !== undefined && (row.color !== lane.color || Math.min(row.lane, maxLanes - 1) !== lane.lane)
  return (
    <svg aria-hidden="true" viewBox={`0 0 ${unit * lanes} ${height}`} className={`${className} w-auto shrink-0`}>
      {row.above.map((e, i) => line(e, 0, e.to === row.lane ? nodeY - gap : nodeY, `a${i}`, faded(e)))}
      {row.below.map((e, i) => line(e, e.from === row.lane ? nodeY + gap : nodeY, height, `b${i}`, faded(e)))}
      {row.merge ? (
        <circle
          cx={cx}
          cy={nodeY}
          r={ring - 1}
          className={`lane-${row.color}`}
          stroke="currentColor"
          strokeWidth={2}
          fill="none"
          opacity={nodeFaded ? 0.3 : undefined}
        />
      ) : (
        <circle
          cx={cx}
          cy={nodeY}
          r={4}
          className={`lane-${row.color}`}
          fill="currentColor"
          opacity={nodeFaded ? 0.3 : undefined}
        />
      )}
    </svg>
  )
}
