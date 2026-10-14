// Display hygiene for untrusted one-line strings (commit subjects, author names, repository titles, tags). Rendering is always React text
// (escaped); this only removes characters that can reorder or hide text, and caps the length (a list shows a subject line, never a whole message body).

/** Bidirectional overrides and isolates (U+202A..202E, U+2066..2069): "Trojan Source" style spoofing in a list of names. */
const BIDI_CONTROLS = new RegExp("[\\u202A-\\u202E\\u2066-\\u2069]", "gu")
/** C0 and C1 controls except tab; newline is handled by firstLine(). */
const CONTROLS = new RegExp("[\\u0000-\\u0008\\u000B-\\u001F\\u007F-\\u009F]", "gu")

const LINE_BREAK = new RegExp("[\\r\\n\\u2028\\u2029]", "u")

/** Text up to the first line break, trimmed. */
export function firstLine(s: string): string {
  const i = s.search(LINE_BREAK)
  return (i === -1 ? s : s.slice(0, i)).trim()
}

const seg = new Intl.Segmenter(undefined, { granularity: "grapheme" })

/** Truncates to `max` grapheme clusters (never splits an emoji or a combining sequence) and appends one ellipsis character. */
export function truncateGraphemes(s: string, max: number): string {
  if (s.length <= max) return s // UTF-16 length is an upper bound of the grapheme count
  const parts: string[] = []
  for (const { segment } of seg.segment(s)) {
    if (parts.length === max) return parts.join("") + "\u2026"
    parts.push(segment)
  }
  return s
}

/** One safe display line: first line, controls and bidi overrides removed, capped. The full original stays available as the `title` attribute. */
export function displayText(s: string | null | undefined, max = 200): string {
  if (s == null) return ""
  return truncateGraphemes(firstLine(s).replace(BIDI_CONTROLS, "").replace(CONTROLS, ""), max)
}
