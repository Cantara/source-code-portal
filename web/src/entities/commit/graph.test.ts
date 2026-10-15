import { describe, expect, it } from "vitest"
import { graph, maxLanes } from "./graph"

const c = (sha: string, ...parents: string[]) => ({ sha, parents })
const lines = (es: { from: number; to: number }[]) => es.map((e) => `${e.from}>${e.to}`).sort()

describe("graph", () => {
  it("draws a linear history as one lane", () => {
    const g = graph([c("c", "b"), c("b", "a"), c("a")])
    expect(g.lanes).toBe(1)
    expect(g.rows.map((r) => [r.lane, lines(r.above), lines(r.below)])).toEqual([
      [0, [], ["0>0"]],
      [0, ["0>0"], ["0>0"]],
      [0, ["0>0"], []],
    ])
    expect(g.rows.every((r) => !r.merge && r.color === 0)).toBe(true)
  })

  it("opens a lane for a merged branch and converges it at the fork", () => {
    const g = graph([c("m", "b1", "f1"), c("f1", "b0"), c("b1", "b0"), c("b0")])
    expect(g.lanes).toBe(2)
    expect(g.rows.map((r) => [r.lane, r.merge, lines(r.above), lines(r.below)])).toEqual([
      [0, true, [], ["0>0", "0>1"]],
      [1, false, ["0>0", "1>1"], ["0>0", "1>1"]],
      [0, false, ["0>0", "1>1"], ["0>0", "1>1"]],
      [0, false, ["0>0", "1>0"], []],
    ])
    expect(g.rows[1]?.color).not.toBe(g.rows[0]?.color)
  })

  it("joins a merge parent another lane already expects", () => {
    const g = graph([c("m2", "a", "x"), c("m1", "a", "x"), c("x", "a"), c("a")])
    // m1 opens lane 2; its first parent continues there, x joins lane 1, which passes through.
    expect(g.rows[1]?.lane).toBe(2)
    expect(lines(g.rows[1]?.below ?? [])).toEqual(["0>0", "1>1", "2>1", "2>2"])
    // x continues lane 1 to a, so all three lanes converge at a.
    expect(lines(g.rows[3]?.above ?? [])).toEqual(["0>0", "1>0", "2>0"])
  })

  it("marks lines to parents outside the rows as open", () => {
    const g = graph([c("b", "a"), c("a", "z")])
    expect(g.rows[0]?.below).toEqual([{ from: 0, to: 0, color: 0, open: false }])
    expect(g.rows[1]?.below).toEqual([{ from: 0, to: 0, color: 0, open: true }])
  })

  it("starts an unexpected commit in a free lane and bounds the lanes", () => {
    expect(graph([c("b", "a"), c("x"), c("a")]).rows.map((r) => r.lane)).toEqual([0, 1, 0])
    const wide = graph([c("o", ...Array.from({ length: 12 }, (_, i) => `p${i}`))])
    expect(wide.lanes).toBe(8)
  })
})

// A random history in graph order: commit i's parents are later commits, some beyond the history.
function randomHistory(seed: number, n: number) {
  let s = seed
  const rnd = (k: number) => {
    s = (s * 1103515245 + 12345) % 2147483648
    return s % k
  }
  return Array.from({ length: n }, (_, i) => {
    const parents = new Set<string>()
    const count = i === n - 1 ? 0 : rnd(5) === 0 ? 2 + rnd(3) : 1
    for (let j = 0; j < count; j++)
      parents.add(i + 1 + rnd(n - i - 1) < n && rnd(10) > 0 ? `c${i + 1 + rnd(n - i - 1)}` : `old${rnd(4)}`)
    const ps = [...parents]
    return { sha: `c${i}`, parents: ps, beyond: ps.filter((p) => p.startsWith("old")) }
  })
}

describe("graph on random histories", () => {
  it("keeps earlier rows identical as more rows load, and every line meets a node or goes on", () => {
    for (let seed = 1; seed <= 150; seed++) {
      const all = randomHistory(seed, 2 + (seed % 90))
      const full = graph(all)
      expect(full.lanes).toBeLessThanOrEqual(maxLanes)
      for (let k = 1; k < all.length; k += 1 + (seed % 7)) {
        expect(graph(all.slice(0, k)).rows).toEqual(full.rows.slice(0, k))
      }
      full.rows.forEach((r, i) => {
        for (const e of r.above) expect(e.to === r.lane || e.to === e.from).toBe(true)
        for (const e of r.below) expect(e.from === r.lane || e.to === e.from).toBe(true)
        // A line out of a row comes into the next one at the same lane.
        const next = full.rows[i + 1]
        if (next) for (const e of r.below) expect(next.above.some((a) => a.from === e.to)).toBe(true)
      })
    }
  })
})
