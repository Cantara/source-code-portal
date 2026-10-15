/** maxLanes is how many lanes the graph draws at most; a commit beyond them sits in the last. */
export const maxLanes = 8

/** Edge is a line through a row, from a lane at its top (or its node) to a lane at its bottom (or its node). */
export interface Edge {
  from: number
  to: number
  /** colour index of the lane the line belongs to, 0..5 */
  color: number
  /** the line leads to a parent outside the rows */
  open: boolean
}

/** GraphRow is how one commit's row draws: its node, the lines in from above and the lines out below. */
export interface GraphRow {
  lane: number
  color: number
  merge: boolean
  above: Edge[]
  below: Edge[]
}

export interface Graph {
  rows: GraphRow[]
  /** lanes in use, at most maxLanes */
  lanes: number
}

/**
 * graph lays out commits (newest first) like `git log --graph`: a commit takes the lane that expects it (else the first free one),
 * every other lane expecting it converges into it, its first parent continues the lane, and each further parent opens a lane or joins
 * the one already expecting it. Freed lanes are reused; lanes never shift.
 */
export function graph(commits: readonly { sha: string; parents: readonly string[] }[]): Graph {
  const known = new Set(commits.map((c) => c.sha))
  const lanes: (string | null)[] = []
  const colors: number[] = []
  let opened = 0
  const open = (lane: number) => {
    colors[lane] = lane === 0 && opened === 0 ? 0 : 1 + (opened % 5)
    opened++
  }
  const free = () => {
    const i = lanes.indexOf(null)
    return i < 0 ? lanes.length : i
  }
  let width = 0
  const rows = commits.map((c) => {
    const before = lanes.slice()
    let lane = before.indexOf(c.sha)
    if (lane < 0) {
      lane = free()
      open(lane)
    }
    const above: Edge[] = []
    before.forEach((sha, j) => {
      if (sha === null) return
      above.push({ from: j, to: sha === c.sha ? lane : j, color: colors[j] ?? 0, open: false })
      if (sha === c.sha) lanes[j] = null
    })
    const [first, ...rest] = c.parents
    lanes[lane] = first ?? null
    const fromNode = new Map<number, string>()
    if (first !== undefined) fromNode.set(lane, first)
    for (const p of rest) {
      let k = lanes.indexOf(p)
      if (k < 0) {
        k = free()
        lanes[k] = p
        open(k)
      }
      fromNode.set(k, p)
    }
    const below: Edge[] = []
    lanes.forEach((sha, j) => {
      if (sha === null) return
      const color = colors[j] ?? 0
      const out = !known.has(sha)
      if (fromNode.has(j)) below.push({ from: lane, to: j, color, open: out })
      if (!fromNode.has(j) || (j !== lane && before[j] === sha)) below.push({ from: j, to: j, color, open: out })
    })
    while (lanes.length > 0 && lanes[lanes.length - 1] === null) lanes.pop()
    width = Math.max(width, lane + 1, ...below.map((e) => e.to + 1), ...above.map((e) => e.from + 1))
    return { lane, color: colors[lane] ?? 0, merge: c.parents.length > 1, above, below }
  })
  return { rows, lanes: Math.min(width, maxLanes) }
}
