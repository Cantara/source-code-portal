import { t } from "@lingui/core/macro"

/** Stable `code` -> localised title and hint. Unknown codes fall back to the English problem title (never `detail`). */
export function problemTitle(code: string): string {
  switch (code) {
    case "not-found":
      return t`Not found`
    case "feature-disabled":
      return t`This feature is switched off`
    case "not-ready":
      return t`The portal is still warming up`
    case "upstream-unavailable":
      return t`GitHub data is temporarily unavailable`
    case "rate-limited":
      return t`Too many requests`
    case "queue-full":
      return t`The portal is busy`
    case "validation-failed":
    case "invalid-cursor":
      return t`That request was not valid`
    case "unauthorized":
      return t`Sign-in required`
    case "forbidden":
      return t`Not allowed`
    case "client-network":
      return t`Cannot reach the portal`
    case "client-unparseable":
      return t`The server sent an unexpected response`
    case "client-aborted":
      return t`Request cancelled`
    case "internal":
      return t`Something went wrong on our side`
    default:
      return t`Unexpected problem`
  }
}
