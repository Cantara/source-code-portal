import { useId } from "react"
import { ArrowUpRightIcon, XIcon } from "@/components/ui/icons"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { relative } from "@/lib/format"

export type Notice = Schemas["Announcement"]

/** Written is a time's wall clock in the offset it was written with: 18:00 at +01:00 is 18:00 on every screen. */
export type Written = { year: number; month: number; day: number; hour: number; minute: number }

/** asWritten reads an RFC 3339 time's own date and clock, never converted to the reader's or the server's zone. */
export function asWritten(iso: string): Written | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(iso)
  if (!m) return undefined
  const [year, month, day, hour, minute] = m.slice(1).map(Number) as [number, number, number, number, number]
  return { year, month, day, hour, minute }
}

const at = (w: Written) => new Date(Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute))
const fmt = (lang: string | undefined, o: Intl.DateTimeFormatOptions) => {
  try {
    return new Intl.DateTimeFormat(lang ?? "en", { ...o, timeZone: "UTC" })
  } catch {
    return new Intl.DateTimeFormat("en", { ...o, timeZone: "UTC" })
  }
}

/** when is the announcement's time in its own language: "tor. 26. nov. 18:00–23:00", a second date when it ends another day. */
export function when(a: Notice): string | undefined {
  const s = a.startsAt ? asWritten(a.startsAt) : undefined
  if (!s) return undefined
  const date = fmt(a.lang, { weekday: "short", day: "numeric", month: "short" })
  const clock = fmt(a.lang, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" })
  const e = a.endsAt ? asWritten(a.endsAt) : undefined
  const start = `${date.format(at(s))} ${clock.format(at(s))}`
  if (!e) return start
  const sameDay = e.year === s.year && e.month === s.month && e.day === s.day
  return `${start}–${sameDay ? "" : date.format(at(e)) + " "}${clock.format(at(e))}`
}

/** countdown is how far the event is: "in 6 days", then "now" while it lasts; nothing after, or without a start. */
export function countdown(a: Notice, now: number): string | undefined {
  if (!a.startsAt) return undefined
  const start = new Date(a.startsAt).getTime()
  const end = a.endsAt ? new Date(a.endsAt).getTime() : start
  if (now >= start && now < end) return "now"
  return start > now ? relative(a.startsAt, now) : undefined
}

/** tones name each tone in words, so colour is never its only sign. */
export const tones: Record<Notice["tone"], string> = { event: "Event", notice: "Notice", info: "Announcement" }

/** shown is the announcements still within their time at now, without the ones the reader dismissed. */
export const shown = (list: Notice[], now: number, dismissed: string[] = []) =>
  list.filter((a) => new Date(a.until).getTime() > now && !dismissed.includes(`${a.id}:${a.revision}`))

/**
 * AnnouncementCard is one announcement (design/announcements.md): a bar and a tint in its tone, the calendar tile of its start, its
 * tone in words, the title one step larger, its line, when · where · how far, and its link as a button. It is its own container:
 * narrow, it gives up the line, then the place, never the title, date or link. On a wall (qr) the link's QR code sits beside the
 * button where there is room. onDismiss adds the ×.
 */
export function AnnouncementCard({
  a,
  now,
  onDismiss,
  qr = false,
  className,
}: {
  a: Notice
  now: number
  onDismiss?: () => void
  qr?: boolean
  className?: string
}) {
  const id = useId()
  const start = a.startsAt ? asWritten(a.startsAt) : undefined
  const time = when(a)
  const soon = countdown(a, now)
  const meta = [time, a.place, soon].filter(Boolean)
  return (
    <article
      aria-labelledby={id}
      className={cn(
        "announcement @container/notice relative rounded-xl border border-l-4 p-4 pr-10",
        `tone-${a.tone}`,
        !onDismiss && "pr-4",
        className,
      )}
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
        {start && (
          <div
            aria-hidden="true"
            className="announcement-tile flex w-14 shrink-0 flex-col items-center rounded-lg py-1.5 @3xl/notice:w-16"
          >
            <span className="text-2xl leading-none font-bold tabular-nums @3xl/notice:text-3xl">{start.day}</span>
            <span className="mt-0.5 text-[0.6875rem] font-semibold tracking-widest uppercase">
              {fmt(a.lang, { month: "short" }).format(at(start)).replace(".", "")}
            </span>
          </div>
        )}
        <div className="min-w-0 flex-1 basis-56">
          <p className="text-[0.6875rem] font-semibold tracking-widest text-[var(--tone-fg)] uppercase">{tones[a.tone]}</p>
          <h3 id={id} lang={a.lang} className="text-lg leading-snug font-semibold text-balance @3xl/notice:text-xl">
            {a.title}
          </h3>
          {a.text && (
            <p lang={a.lang} className="mt-0.5 text-sm text-foreground/85 @max-sm/notice:hidden @3xl/notice:text-base">
              {a.text}
            </p>
          )}
          {meta.length > 0 && (
            <p className="mt-1 flex flex-wrap gap-x-1.5 text-sm text-muted-foreground">
              {time && (
                <time lang={a.lang} dateTime={a.startsAt}>
                  {time}
                </time>
              )}
              {a.place && (
                <span lang={a.lang} className="@max-xs/notice:hidden">
                  {time && <span aria-hidden="true">· </span>}
                  {a.place}
                </span>
              )}
              {soon && (
                <span className="font-medium text-[var(--tone-fg)]">
                  {(time || a.place) && <span aria-hidden="true">· </span>}
                  {soon}
                </span>
              )}
            </p>
          )}
        </div>
        {a.link && (
          <div className="flex shrink-0 items-center gap-3">
            <a
              href={a.link.url}
              rel="noopener noreferrer"
              target="_blank"
              lang={a.lang}
              className="announcement-action inline-flex items-center gap-1.5 rounded-lg px-3.5 py-2 text-sm font-semibold shadow-sm transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
            >
              {a.link.label}
              <ArrowUpRightIcon className="size-4" />
            </a>
            {qr && a.qr && (
              // Black on white whatever the theme: a phone's camera reads a code the other way round badly.
              <svg
                role="img"
                aria-label={`QR code for ${a.link.label}`}
                viewBox={`-2 -2 ${a.qr.size + 4} ${a.qr.size + 4}`}
                shapeRendering="crispEdges"
                className="hidden size-24 shrink-0 rounded-md bg-white @2xl/notice:block @4xl/notice:size-28"
              >
                <path d={a.qr.path} className="fill-black" />
              </svg>
            )}
          </div>
        )}
      </div>
      {onDismiss && (
        <button
          type="button"
          onClick={onDismiss}
          aria-label={`Dismiss “${a.title}”`}
          className="absolute top-2 right-2 rounded-md p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        >
          <XIcon className="size-4" />
        </button>
      )}
    </article>
  )
}
