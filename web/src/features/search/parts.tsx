import type { TextPart } from "./api"

/** Parts draws a title or a snippet the search returned: plain text, the matches marked. Nothing in it is markup. */
export function Parts({ parts }: { parts: TextPart[] }) {
  return parts.map((p, i) =>
    p.mark ? (
      <mark key={i} className="rounded-sm bg-primary/15 px-0.5 font-semibold text-foreground">
        {p.text}
      </mark>
    ) : (
      <span key={i}>{p.text}</span>
    ),
  )
}

/** plain is the parts as one string. */
export const plain = (parts: TextPart[]) => parts.map((p) => p.text).join("")
