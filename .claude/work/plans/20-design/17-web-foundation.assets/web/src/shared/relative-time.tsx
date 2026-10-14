import { formatDateTime, formatRelative, useDisplayZone, useNow } from "@/lib/format"

/** `<time>` with a relative label that advances with the shared ticker; the title is the exact time in the display zone. */
export function RelativeTime({ iso, zone }: { iso: string; zone?: string | undefined }) {
  const now = useNow()
  const displayZone = useDisplayZone()
  return (
    <time dateTime={iso} title={formatDateTime(iso, zone ?? displayZone)}>
      {formatRelative(iso, now)}
    </time>
  )
}
