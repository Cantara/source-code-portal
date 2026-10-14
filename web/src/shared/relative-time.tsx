"use client"

import { useNow } from "@/lib/clock"
import { exactUTC, relative } from "@/lib/format"

/** RelativeTime shows "3 days ago" with the exact UTC instant in the element and its title. */
export function RelativeTime({ at }: { at: string | null | undefined }) {
  const now = useNow()
  if (!at) return <span>never</span>
  return (
    <time dateTime={at} title={exactUTC(at)} suppressHydrationWarning>
      {relative(at, now)}
    </time>
  )
}
