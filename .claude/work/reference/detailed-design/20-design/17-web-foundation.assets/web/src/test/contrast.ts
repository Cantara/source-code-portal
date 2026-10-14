// OKLCH -> sRGB and WCAG contrast (port of 05-research/08-prototypes/contrast.py). Pure; no DOM.
export type Rgb = [number, number, number] // linear-light sRGB 0..1
const clamp = (x: number) => Math.max(0, Math.min(1, x))

export function oklchToLinear(L: number, C: number, hDeg: number): Rgb {
  const a = C * Math.cos((hDeg * Math.PI) / 180)
  const b = C * Math.sin((hDeg * Math.PI) / 180)
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b
  const s_ = L - 0.0894841775 * a - 1.291485548 * b
  const l = l_ ** 3,
    m = m_ ** 3,
    s = s_ ** 3
  return [
    clamp(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    clamp(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    clamp(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
  ]
}
const enc = (x: number) => (x <= 0.0031308 ? 12.92 * x : 1.055 * x ** (1 / 2.4) - 0.055)
const dec = (x: number) => (x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4)
/** Browsers composite in gamma-encoded sRGB. */
export function over(fg: Rgb, bg: Rgb, alpha: number): Rgb {
  return fg.map((c, i) => dec(enc(c) * alpha + enc(bg[i] as number) * (1 - alpha))) as Rgb
}
const lum = ([r, g, b]: Rgb) => 0.2126 * r + 0.7152 * g + 0.0722 * b
export function contrast(a: Rgb, b: Rgb): number {
  const [hi, lo] = lum(a) >= lum(b) ? [lum(a), lum(b)] : [lum(b), lum(a)]
  return (hi + 0.05) / (lo + 0.05)
}

const OKLCH = /^oklch\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*(?:\/\s*([\d.]+)%)?\s*\)$/
/** Parses `oklch(L C H / A%)`; a lone `oklch(1 0 0)` is white. Returns colour and alpha. */
export function parseOklch(v: string): { rgb: Rgb; alpha: number } {
  const m = OKLCH.exec(v.trim())
  if (!m) throw new Error(`unsupported colour ${v}`)
  return { rgb: oklchToLinear(Number(m[1]), Number(m[2]), Number(m[3])), alpha: m[4] ? Number(m[4]) / 100 : 1 }
}
/** Reads `--name: value;` declarations of one selector block from a CSS string. */
export function tokens(css: string, selector: string): Record<string, string> {
  const start = css.indexOf(`${selector} {`)
  if (start < 0) throw new Error(`block ${selector} not found`)
  const body = css.slice(css.indexOf("{", start) + 1, css.indexOf("}", start))
  const out: Record<string, string> = {}
  for (const m of body.matchAll(/--([\w-]+):\s*([^;]+);/g)) out[m[1] as string] = (m[2] as string).trim()
  return out
}
