// @vitest-environment node
// (unit 17's "unit" Vitest project runs src/**/*.test.ts in jsdom, whose URL class refuses `new URL("./tokens.css", import.meta.url)`: this test reads a file, so it asks for node, V-08)
// Ports tokens/contrast.py: every foreground/background pair of styles/tokens.css must meet WCAG 2.2 AA in both themes (SC 1.4.3, 1.4.11).
import { readFileSync } from "node:fs"
import { describe, expect, it } from "vitest"

type Lch = [number, number, number]
function parse(css: string, selector: string): Record<string, string> {
  const m = new RegExp(`${selector.replace(".", "\\.")}\\s*\\{([^}]*)\\}`, "g")
  const out: Record<string, string> = {}
  for (const b of css.matchAll(m)) for (const d of (b[1] ?? "").matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)) out[d[1]!] = d[2]!.trim()
  return out
}
function oklch(v: string): Lch { const m = /oklch\(([\d.]+) ([\d.]+) ([\d.]+)\)/.exec(v); if (!m) throw new Error(`not oklch: ${v}`); return [Number(m[1]), Number(m[2]), Number(m[3])] }
function lin([L, C, h]: Lch): [number, number, number] {
  const a = C * Math.cos((h * Math.PI) / 180), b = C * Math.sin((h * Math.PI) / 180)
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3, m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3, s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3
  const c = (x: number) => Math.max(0, Math.min(1, x))
  return [c(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s), c(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s), c(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s)]
}
const Y = (c: [number, number, number]) => 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
const ratio = (a: string, b: string) => { const [x, y] = [Y(lin(oklch(a))), Y(lin(oklch(b)))]; return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05) }

const css = readFileSync(new URL("./tokens.css", import.meta.url), "utf8")
const themes = { light: parse(css, ":root"), dark: { ...parse(css, ":root"), ...parse(css, ".dark") } }
const pairs: [string, string, number][] = [
  ["foreground", "background", 4.5], ["foreground", "card", 4.5], ["muted-foreground", "background", 4.5], ["muted-foreground", "muted", 4.5], ["muted-foreground", "card", 4.5],
  ["muted-foreground", "sidebar", 4.5], ["primary-foreground", "primary", 4.5], ["link", "background", 4.5], ["accent-foreground", "accent", 4.5],
  ["input", "background", 3], ["input", "card", 3], ["ring", "background", 3], ["ring", "card", 3], ["ring", "muted", 3],
  ["success", "success-bg", 4.5], ["warning", "warning-bg", 4.5], ["danger", "danger-bg", 4.5], ["info", "info-bg", 4.5],
  ["success", "background", 4.5], ["warning", "background", 4.5], ["danger", "background", 4.5], ["info", "background", 4.5],
]
describe.each(Object.entries(themes))("%s theme", (_name, t) => {
  it.each(pairs)("%s on %s >= %f", (fg, bg, min) => expect(ratio(t[fg]!, t[bg]!)).toBeGreaterThanOrEqual(min))
  it("viz colours are >= 3:1 on the card (graphical objects)", () => { for (let i = 1; i <= 8; i++) expect(ratio(t[`viz-${i}`]!, t["card"]!)).toBeGreaterThanOrEqual(3) })
})
it("avatar backgrounds carry white text >= 4.5:1", () => { for (let i = 1; i <= 8; i++) expect(ratio(themes.light[`avatar-${i}`]!, "oklch(1 0 0)")).toBeGreaterThanOrEqual(4.5) })
