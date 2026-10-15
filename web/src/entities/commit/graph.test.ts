import { describe, expect, it } from "vitest"
import { graph } from "./graph"

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
