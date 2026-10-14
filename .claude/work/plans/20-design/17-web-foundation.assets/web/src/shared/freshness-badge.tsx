import { t } from "@lingui/core/macro"
import { AlertTriangle, CheckCircle2, CircleHelp, Clock, type LucideIcon } from "lucide-react"
import type { components } from "@/lib/api/schema"
import { Badge } from "@/components/ui/badge"
import { RelativeTime } from "./relative-time"

type Freshness = components["schemas"]["Freshness"]
type Status = Freshness["status"]

const ICON: Record<Status, LucideIcon> = { fresh: CheckCircle2, stale: Clock, error: AlertTriangle, unknown: CircleHelp }

function label(status: Status): string {
  switch (status) {
    case "fresh":
      return t`Up to date`
    case "stale":
      return t`Delayed`
    case "error":
      return t`Update failing`
    case "unknown":
      return t`Not loaded yet`
    default:
      return t`Unknown`
  }
}

/** Status is always icon + text, never colour alone. Enum is open: an unknown value renders as "Unknown". */
export function FreshnessBadge({ freshness }: { freshness: Freshness }) {
  const Icon = (ICON as Partial<Record<string, LucideIcon>>)[freshness.status] ?? CircleHelp
  return (
    <Badge variant={freshness.status === "fresh" ? "secondary" : "outline"} data-freshness={freshness.status}>
      <Icon aria-hidden="true" />
      <span>{label(freshness.status)}</span>
      {freshness.asOf ? (
        <span className="text-muted-foreground">
          <RelativeTime iso={freshness.asOf} />
        </span>
      ) : null}
    </Badge>
  )
}
