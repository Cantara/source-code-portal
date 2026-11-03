import type { Commit } from "@/lib/api/client"

/**
 * AiMark marks a commit an AI helped make: "AI" for a person with an AI co-author, "agent" for a coding agent as the author, the AI
 * the commit names as the hover text. Nothing for a person's own commit or automation's.
 */
export function AiMark({ commit }: { commit: Pick<Commit, "kind" | "assistant"> }) {
  if (commit.kind !== "assisted" && commit.kind !== "agent") return null
  const text = commit.kind === "agent" ? "agent" : "AI"
  const about =
    commit.kind === "agent"
      ? `by a coding agent${commit.assistant ? `, ${commit.assistant}` : ""}`
      : `with ${commit.assistant || "an AI"}`
  return (
    <span
      title={about}
      className="inline-flex items-center rounded border border-[color-mix(in_oklch,var(--ai)_45%,transparent)] px-1 text-[0.6875rem] leading-4 font-medium text-[var(--ai)]"
    >
      <span aria-hidden="true">{text}</span>
      <span className="sr-only">{about}</span>
    </span>
  )
}
