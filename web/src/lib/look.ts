/**
 * The deployment's look (design/theme.md, P-108; named look in code, for `theme` is the light and dark choice of lib/theme.ts): the product name and the addresses of the stylesheet, logo and favicon the
 * portal serves from `web.theme_dir`, which Go passes to this server as the JSON in SCP_THEME. Without it: the built-in look.
 */
export interface Look {
  /** name is what the pages call the product. */
  name: string
  /** css, logo and favicon are addresses under /theme/, with the content's hash. */
  css?: string
  logo?: string
  favicon?: string
}

/** productName is the product's own name, which a deployment may replace. */
export const productName = "Source Code Portal"

const maxName = 60

/** address keeps a theme address only when it is under /theme/: the environment is the portal's own, but a page links nothing else. */
function address(v: unknown): string | undefined {
  return typeof v === "string" && v.startsWith("/theme/") && !v.startsWith("//") && v.length < 200 ? v : undefined
}

/** parseLook reads SCP_THEME defensively: anything else than the expected shape is the built-in look. */
export function parseLook(raw: string | undefined): Look {
  let v: unknown
  try {
    v = raw ? JSON.parse(raw) : undefined
  } catch {
    v = undefined
  }
  const o = v && typeof v === "object" ? (v as Record<string, unknown>) : {}
  const name = typeof o.name === "string" ? o.name.trim().slice(0, maxName) : ""
  return { name: name || productName, css: address(o.css), logo: address(o.logo), favicon: address(o.favicon) }
}

let held: Look | undefined

/** look is this server's look, read once. In the browser the environment is empty: components there read it from the provider. */
export function look(): Look {
  held ??= parseLook(process.env.SCP_THEME)
  return held
}
