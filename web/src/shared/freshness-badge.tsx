import { Badge } from "@/components/ui/badge"
import type { Freshness } from "@/lib/api/client"
import { exactUTC } from "@/lib/format"

const labels = {
  fresh: { text: "Up to date", icon: "✓", variant: "ok" },
  stale: { text: "Delayed", icon: "◷", variant: "warn" },
  error: { text: "Update failing", icon: "!", variant: "error" },
} as const

/** freshnessLabel is the text for a status; anything unexpected is "Unknown". */
export function freshnessLabel(status: string | undefined): string {
  return status && status in labels ? labels[status as keyof typeof labels].text : "Unknown"
}

export function FreshnessBadge({ freshness }: { freshness?: Freshness }) {
  const l = freshness && freshness.status in labels ? labels[freshness.status as keyof typeof labels] : undefined
  const checked = freshness?.checkedAt ? `Checked ${exactUTC(freshness.checkedAt)}` : "Not checked yet"
  return (
    <Badge variant={l?.variant ?? "default"} title={checked}>
      <span aria-hidden="true">{l?.icon ?? "?"}</span>
      {l?.text ?? "Unknown"}
    </Badge>
  )
}
