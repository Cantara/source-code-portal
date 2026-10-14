import { Trans } from "@lingui/react/macro"
import { RotateCcw } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { problemFromThrown } from "@/lib/api/problem"
import { problemTitle } from "./problem-title"

const AUTO_RETRY = new Set(["not-ready", "rate-limited", "upstream-unavailable", "queue-full", "client-network"])

/**
 * Counts down visually (aria-hidden: a ticking number must never be announced), calls `onRetry` when it reaches zero and
 * then starts over, so a problem that persists is retried every `seconds`, not on every render. `onRetry` is read through a
 * ref: parents pass a fresh closure on each render and that must not restart or short-circuit the countdown.
 */
function AutoRetry({ seconds, onRetry }: { seconds: number; onRetry: () => void }) {
  const [left, setLeft] = useState(seconds)
  const retry = useRef(onRetry)
  useEffect(() => {
    retry.current = onRetry
  })
  useEffect(() => {
    const id = setTimeout(() => {
      if (left <= 1) {
        retry.current()
        setLeft(seconds)
      } else setLeft(left - 1)
    }, 1000)
    return () => clearTimeout(id)
  }, [left, seconds])
  return <span aria-hidden="true"> ({left})</span>
}

export function ProblemAlert({ error, onRetry }: { error: unknown; onRetry?: (() => void) | undefined }) {
  const p = problemFromThrown(error)
  const auto = onRetry !== undefined && AUTO_RETRY.has(p.code)
  return (
    <Alert variant="destructive" role="alert" data-problem-code={p.code}>
      <AlertTitle>{problemTitle(p.code)}</AlertTitle>
      <AlertDescription>
        {p.requestId ? (
          <p>
            <Trans>
              Request id: <code>{p.requestId}</code>
            </Trans>
          </p>
        ) : null}
        {auto ? (
          <p>
            <Trans>Retrying automatically.</Trans>
            <AutoRetry seconds={Math.min(p.retryAfterSeconds ?? 15, 60)} onRetry={onRetry} />
          </p>
        ) : null}
        {onRetry ? (
          <Button variant="outline" size="sm" onClick={onRetry}>
            <RotateCcw aria-hidden="true" />
            <Trans>Try again</Trans>
          </Button>
        ) : null}
      </AlertDescription>
    </Alert>
  )
}
