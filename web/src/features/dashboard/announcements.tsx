"use client"

import { useState } from "react"
import { AnnouncementCard, shown, type Notice } from "@/entities/announcement/announcement"
import { useNow } from "@/lib/clock"
import { dismissedCookie, dismissedValue, remember } from "@/lib/preferences"

/**
 * Announcements is the `announcements` element (design/announcements.md): the ones the server gave this page, while their time
 * lasts. On the dashboard (dismissible) each × is kept in the cookie scp-announcements for its revision, which the server reads, so a
 * dismissed one never paints; on the presentation page (wall) none is dismissible and the links show their QR codes. Nothing to show
 * draws nothing.
 */
export function Announcements({
  list,
  dismissed: initial = [],
  wall = false,
}: {
  list: Notice[]
  dismissed?: string[]
  wall?: boolean
}) {
  const now = useNow()
  const [dismissed, setDismissed] = useState(initial)
  const visible = shown(list, now, wall ? [] : dismissed)
  if (visible.length === 0) return null
  const dismiss = (a: Notice) => {
    const key = `${a.id}:${a.revision}`
    remember(dismissedCookie, dismissedValue(dismissed, key))
    setDismissed([...dismissed, key])
  }
  return (
    <section aria-label="Announcements" className="flex flex-col gap-3">
      {visible.map((a) => (
        <AnnouncementCard
          key={`${a.id}:${a.revision}`}
          a={a}
          now={now}
          qr={wall}
          onDismiss={wall ? undefined : () => dismiss(a)}
        />
      ))}
    </section>
  )
}
