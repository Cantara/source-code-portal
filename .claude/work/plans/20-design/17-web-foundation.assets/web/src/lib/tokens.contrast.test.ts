/// <reference types="node" />
import { readFileSync } from "node:fs"
import { describe, expect, it } from "vitest"
import { contrast, over, parseOklch, tokens } from "@/test/contrast"

// Read from disk: Vitest blanks every CSS import (including ?raw) when css processing is off.
const css = readFileSync(`${process.cwd()}/src/styles/index.css`, "utf8")
const themes = { light: tokens(css, ":root"), dark: tokens(css, ".dark") } as const

// [foreground token, background token, minimum ratio, WCAG criterion]
const PAIRS: readonly [string, string, number, string][] = [
  ["foreground", "background", 4.5, "1.4.3"],
  ["card-foreground", "card", 4.5, "1.4.3"],
  ["popover-foreground", "popover", 4.5, "1.4.3"],
  ["primary-foreground", "primary", 4.5, "1.4.3"],
  ["secondary-foreground", "secondary", 4.5, "1.4.3"],
  ["muted-foreground", "background", 4.5, "1.4.3"],
  ["muted-foreground", "muted", 4.5, "1.4.3"],
  ["accent-foreground", "accent", 4.5, "1.4.3"],
  ["destructive", "background", 4.5, "1.4.3"],
  ["input", "background", 3, "1.4.11 control boundary"],
  ["ring", "background", 3, "1.4.11 focus indicator"],
]

function colour(theme: "light" | "dark", name: string, over_: string) {
  const t = themes[theme]
  const bg = parseOklch(t[over_] ?? "oklch(1 0 0)")
  const c = parseOklch(t[name] as string)
  return c.alpha < 1 ? over(c.rgb, bg.rgb, c.alpha) : c.rgb
}

describe.each(["light", "dark"] as const)("token contrast (%s)", (theme) => {
  it.each(PAIRS)("%s on %s >= %s (%s)", (fg, bg, min) => {
    const ratio = contrast(colour(theme, fg, bg), colour(theme, bg, "background"))
    expect(ratio, `${theme} ${fg} on ${bg}`).toBeGreaterThanOrEqual(min)
  })
})
