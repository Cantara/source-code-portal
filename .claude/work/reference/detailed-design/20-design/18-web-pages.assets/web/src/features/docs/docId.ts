import type { components } from "@/lib/api/schema"
type DocIndexEntry = components["schemas"]["DocIndexEntry"]

// The contract's `DocId` (07 common schema): `readme`, or the repository-relative path with every `/` replaced by `~`, and every byte of the path outside
// [A-Za-z0-9._-] (including a literal `~` and `%`, and every UTF-8 byte of a non-ASCII name) written `%XX` with upper-case hex. The encoding is injective.
// V-04: the first draft rejected every path with a space or a non-ASCII letter ("docs/Release Notes.md", "docs/Økonomi.md") and mapped `~` back with a
// plain split, so any such document that was not found in the index could not even be asked for, and `splatFromDocId` returned `%20` undecoded.
const MAX_ID = 1024
const encoder = new TextEncoder()
const SAFE_BYTE = (b: number): boolean => (b >= 0x30 && b <= 0x39) || (b >= 0x41 && b <= 0x5a) || (b >= 0x61 && b <= 0x7a) || b === 0x2e || b === 0x5f || b === 0x2d
const LONE_SURROGATE = /[\ud800-\udfff]/u // with the `u` flag a paired surrogate is one code point, so this matches only a lone one (no dependency on String.prototype.isWellFormed, lib ES2024)
// eslint-disable-next-line no-control-regex -- the point is to refuse control characters
const CONTROL = /[\u0000-\u001F\u007F-\u009F]/u

function encodeSegment(seg: string): string {
  let out = ""
  for (const b of encoder.encode(seg)) out += SAFE_BYTE(b) ? String.fromCharCode(b) : `%${b.toString(16).toUpperCase().padStart(2, "0")}`
  return out
}

/** Splat of /repos/$repo/docs/$ (repository-relative path, as the router decoded it) -> API doc id. null = not a legal path (empty or `.`/`..` segment, control character, lone surrogate, id longer than 1,024). */
export function docIdFromSplat(splat: string): string | null {
  if (splat === "" || LONE_SURROGATE.test(splat) || CONTROL.test(splat)) return null
  const segs = splat.split("/")
  for (const s of segs) if (s === "" || s === "." || s === "..") return null
  const id = segs.map(encodeSegment).join("~")
  return id.length > MAX_ID ? null : id
}

/** Inverse of `docIdFromSplat` (`readme` is not a path: the caller special-cases it). null for a malformed id. */
export function splatFromDocId(id: string): string | null {
  const dec = new TextDecoder("utf-8", { fatal: true })
  try {
    return id.split("~").map((seg) => {
      const bytes: number[] = []
      for (let i = 0; i < seg.length; i++) {
        const c = seg.charAt(i)
        if (c === "%") {
          const hex = seg.slice(i + 1, i + 3)
          if (!/^[0-9A-F]{2}$/.test(hex)) throw new URIError("bad escape")
          bytes.push(Number.parseInt(hex, 16)); i += 2
        } else bytes.push(...encoder.encode(c))
      }
      return dec.decode(new Uint8Array(bytes))
    }).join("/")
  } catch {
    return null
  }
}

const DOC_EXT = /\.(md|markdown|adoc|asciidoc)$/i

/** The UI URL of a document drops its Markdown/AsciiDoc extension, so it can never collide with the raw route `.../docs/{path}.md` (deviation D-18-01, accepted by unit 10 as its D-9). */
export function stripDocExt(path: string): string {
  return path.replace(DOC_EXT, "")
}

/** Splat to use in `/repos/$repo/docs/$` for an index entry. */
export const docSplat = (e: Pick<DocIndexEntry, "path">): string => stripDocExt(e.path)

/**
 * Resolve the document the URL names. The index wins (a README at /docs/README.md maps to id `readme`);
 * the path -> id rule is only the fallback for a document that is not in the index (then the API answers 404 or not_resident).
 * null = the splat is not a legal path: the caller renders `ProblemPage` kind not-found (resource "document"), it never calls the API.
 */
export function resolveDocId(splat: string, index: readonly DocIndexEntry[]): string | null {
  // accepts the canonical extension-less form and the full path (hand-written links, rewritten links, a typed URL)
  const hit = index.find((e) => e.path === splat) ?? index.find((e) => stripDocExt(e.path) === splat)
  if (hit) return hit.id
  return docIdFromSplat(splat)
}
