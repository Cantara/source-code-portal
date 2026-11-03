"use client"

import type { Commit } from "@/lib/api/client"
import { Hint } from "@/shared/hint"

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
    <Hint
      heading={commit.kind === "agent" ? "A coding agent's commit" : "Made with AI"}
      swatch="ai-swatch"
      meta={commit.assistant || undefined}
      render={(t) => (
        <span
          {...t}
          className="inline-flex cursor-help items-center rounded border border-[color-mix(in_oklch,var(--ai)_45%,transparent)] px-1 text-[0.6875rem] leading-4 font-medium text-[var(--ai)]"
        >
          <span aria-hidden="true">{text}</span>
          <span className="sr-only">{about}</span>
        </span>
      )}
    >
      {commit.kind === "agent"
        ? "An AI agent authored this commit on someone's behalf. It counts as the team's work, with AI."
        : `A person made this commit ${about}: an AI co-author or tool footer in the message. It counts as the team's work, with AI.`}
    </Hint>
  )
}
