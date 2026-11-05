import type { Notice } from "./announcement"

/** juleol is an event announcement as the server serves it, for the tests. */
export const juleol: Notice = {
  id: "juleol",
  revision: "abc123",
  title: "Juleølsmaking på Rebel Makerspace",
  text: "Det avholdes juleølsmaking i år.",
  tone: "event",
  startsAt: "2026-11-26T18:00:00+01:00",
  endsAt: "2026-11-26T23:00:00+01:00",
  place: "Rebel Makerspace",
  link: { label: "Meld deg på her", url: "https://example.org/pamelding" },
  lang: "nb",
  until: "2026-11-26T23:00:00+01:00",
  qr: { size: 21, path: "M0 0h7v1h-7z" },
}

/** now is the tests' clock: six days before the event. */
export const now = Date.parse("2026-11-20T12:00:00Z")
