// Commit authors are shown as initials on a deterministic colour; no network request, no third-party origin (A-25, W19).

/** Number of avatar background tokens (--avatar-1 .. --avatar-8 in styles/tokens.css). */
export const AVATAR_COLOURS = 8

const BOT_SUFFIX = /\[bot\]$/i

export function isBot(login: string | null | undefined): boolean {
  return login != null && BOT_SUFFIX.test(login)
}

function graphemes(s: string): string[] {
  // Intl.Segmenter keeps combined emoji / accented letters whole; available in every browser of the support matrix.
  return Array.from(new Intl.Segmenter(undefined, { granularity: "grapheme" }).segment(s), (x) => x.segment)
}

/**
 * Initials of an author: first grapheme of the first and of the last whitespace-separated word of the display name
 * (two letters), or the first grapheme when it is a single word (every login is one word). Falls back to the login, then to "?".
 * Always upper-cased with the locale-independent toUpperCase (avatar text is decorative, the name is printed next to it).
 */
export function initialsOf(authorName: string | null | undefined, authorLogin: string | null | undefined): string {
  const raw = (authorName ?? "").trim() || (authorLogin ?? "").trim()
  const base = raw.replace(BOT_SUFFIX, "").trim()
  const words = base.split(/\s+/u).filter((w) => w.length > 0) // whitespace only: a login like "ole-b" is one word
  if (words.length === 0) return "?"
  const first = graphemes(words[0] ?? "")[0] ?? "?"
  if (words.length === 1) return first.toUpperCase()
  const last = graphemes(words[words.length - 1] ?? "")[0] ?? ""
  return (first + last).toUpperCase()
}

/** FNV-1a 32 bit; stable across sessions and browsers. */
export function fnv1a(s: string): number {
  let h = 0x811c9dc5
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 0x01000193) >>> 0
  }
  return h >>> 0
}

/** 1-based colour index; the key is the lower-cased login when there is one (stable per person), else the name. */
export function avatarColour(authorName: string | null | undefined, authorLogin: string | null | undefined): number {
  const key = ((authorLogin ?? "").trim() || (authorName ?? "").trim() || "?").toLowerCase()
  return (fnv1a(key) % AVATAR_COLOURS) + 1
}
