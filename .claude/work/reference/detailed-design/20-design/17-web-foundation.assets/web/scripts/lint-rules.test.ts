import { ESLint, type Linter } from "eslint"
import { describe, expect, it } from "vitest"
// @ts-expect-error eslint.config.js is plain JS without declarations
import config from "../eslint.config.js"

// Each case lints a snippet as if it lived at `file` and asserts which rule fires. These pin the architectural rules of
// web/eslint.config.js (feature boundaries, URL strings, HTML sinks, client-side data access).
const cwd = process.cwd() // vitest runs from web/ (pnpm test)
// The snippets do not exist on disk, so the type-aware project service cannot load them: lint with the real config minus
// the type-checked @typescript-eslint rules (those are exercised by `pnpm lint` itself).
const untyped = (config as Linter.Config[]).map((c) => {
  const out: Linter.Config = { ...c }
  if (c.languageOptions)
    out.languageOptions = { ...c.languageOptions, parserOptions: { ...(c.languageOptions.parserOptions ?? {}), projectService: false } }
  if (c.rules) out.rules = Object.fromEntries(Object.entries(c.rules).filter(([k]) => !k.startsWith("@typescript-eslint/")))
  return out
})
const eslint = new ESLint({ cwd, overrideConfigFile: true, baseConfig: untyped })
async function rules(file: string, code: string): Promise<string[]> {
  const [r] = await eslint.lintText(code, { filePath: `${cwd}/${file}` })
  return [...new Set((r?.messages ?? []).map((m) => m.ruleId ?? "parse-error"))]
}

const CASES: readonly [name: string, file: string, code: string, rule: string][] = [
  [
    "a feature importing another feature's internals",
    "src/features/docs/x.ts",
    'import { groupsQuery } from "@/features/dashboard/api"\nexport const x = groupsQuery\n',
    "boundaries/dependencies",
  ],
  [
    "a lower feature importing a higher one (rank)",
    "src/features/commits/x.ts",
    'import { y } from "@/features/dashboard"\nexport const x = y\n',
    "boundaries/dependencies",
  ],
  [
    "shared importing a feature",
    "src/shared/x.ts",
    'import { y } from "@/features/groups"\nexport const x = y\n',
    "boundaries/dependencies",
  ],
  ["a hard-coded portal path", "src/shared/x.ts", 'export const x = "/repos/core-lib"\n', "no-restricted-syntax"],
  [
    "a hard-coded API path outside lib/api and features/*/api.ts",
    "src/shared/x.ts",
    'export const x = "/api/v1/groups"\n',
    "no-restricted-syntax",
  ],
  [
    "innerHTML outside safe-html",
    "src/shared/x.ts",
    'export const f = (e: HTMLElement) => { e.innerHTML = "a" }\n',
    "no-restricted-syntax",
  ],
  ["raw fetch", "src/shared/x.ts", 'export const f = () => fetch("https://example.org")\n', "no-restricted-globals"],
  ["direct localStorage", "src/shared/x.ts", 'export const f = () => localStorage.getItem("a")\n', "no-restricted-globals"],
  ["full zod instead of zod/mini", "src/lib/x.ts", 'import { z } from "zod"\nexport const s = z.string()\n', "no-restricted-imports"],
  ["react-router", "src/lib/x.ts", 'import { Link } from "react-router"\nexport const l = Link\n', "no-restricted-imports"],
  [
    "an unlocalised user-visible string",
    "src/shared/x.tsx",
    "export const X = () => <p>Hello there</p>\n",
    "lingui/no-unlocalized-strings",
  ],
  [
    "a remote avatar image",
    "src/shared/x.tsx",
    'import { AvatarImage } from "@/components/ui/avatar"\nexport const X = () => <AvatarImage src="https://a.example/x.png" />\n',
    "no-restricted-syntax",
  ],
  [
    "dangerouslySetInnerHTML outside safe-html",
    "src/shared/x.tsx",
    "export const X = ({ h }: { h: string }) => <div dangerouslySetInnerHTML={{ __html: h }} />\n",
    "no-restricted-syntax",
  ],
  [
    "dangerouslySetInnerHTML inside owned shadcn code (an override must not drop the ban)",
    "src/components/ui/x.tsx",
    "export const X = ({ h }: { h: string }) => <div dangerouslySetInnerHTML={{ __html: h }} />\n",
    "no-restricted-syntax",
  ],
  [
    "innerHTML inside lib/api (an override must not drop the ban)",
    "src/lib/api/x.ts",
    'export const f = (e: HTMLElement) => { e.innerHTML = "a" }\n',
    "no-restricted-syntax",
  ],
  ["document.cookie", "src/lib/x.ts", 'export const f = () => { document.cookie = "a=b" }\n', "no-restricted-syntax"],
  ["sessionStorage", "src/shared/x.ts", 'export const f = () => sessionStorage.getItem("a")\n', "no-restricted-globals"],
  ["EventSource outside lib/api", "src/shared/x.ts", 'export const f = () => new EventSource("/api/v1/events")\n', "no-restricted-globals"],
  [
    "SafeHtml fed by something other than an API html field",
    "src/shared/x.tsx",
    'import { SafeHtml } from "@/shared/safe-html"\nexport const X = ({ s }: { s: string }) => <SafeHtml html={s} />\n',
    "no-restricted-syntax",
  ],
]
describe("eslint architecture rules", () => {
  it.each(CASES)(
    "%s",
    async (_name, file, code, rule) => {
      expect(await rules(file, code)).toContain(rule)
    },
    60_000,
  )
  it("allows a feature to import a lower-ranked feature through its index.ts", async () => {
    expect(await rules("src/features/dashboard/x.ts", 'import { y } from "@/features/groups"\nexport const x = y\n')).not.toContain(
      "boundaries/dependencies",
    )
  }, 60_000)
  it("allows endpoint paths in features/*/api.ts", async () => {
    expect(await rules("src/features/groups/api.ts", 'export const p = "/api/v1/groups"\n')).not.toContain("no-restricted-syntax")
  }, 60_000)
})
