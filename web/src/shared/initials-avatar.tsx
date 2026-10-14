const palette = [
  "bg-sky-700",
  "bg-emerald-700",
  "bg-violet-700",
  "bg-rose-700",
  "bg-amber-800",
  "bg-teal-700",
  "bg-indigo-700",
  "bg-fuchsia-700",
]

/** initials is up to two letters of a name: first and last word. */
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return "?"
  const first = [...(words[0] ?? "")][0] ?? ""
  const last = words.length > 1 ? ([...(words.at(-1) ?? "")][0] ?? "") : ""
  return (first + last).toUpperCase()
}

/** colour picks a palette entry from the name, the same one every time. */
export function colour(name: string): string {
  let h = 0
  for (const c of name) h = (h * 31 + (c.codePointAt(0) ?? 0)) >>> 0
  return palette[h % palette.length] ?? "bg-sky-700"
}

export function InitialsAvatar({ name }: { name: string }) {
  return (
    <span
      aria-hidden="true"
      className={`inline-flex size-7 shrink-0 items-center justify-center rounded-full text-xs font-semibold text-white ${colour(name)}`}
    >
      {initials(name)}
    </span>
  )
}
