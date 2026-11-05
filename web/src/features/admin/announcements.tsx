"use client"

import { queryOptions, useQuery, useQueryClient } from "@tanstack/react-query"
import { useId, useState, type ReactNode } from "react"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardTitle } from "@/components/ui/card"
import { AnnouncementCard, asWritten, tones, type Notice } from "@/entities/announcement/announcement"
import { get, ProblemError, send } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"
import { useNow } from "@/lib/clock"
import { cn } from "@/lib/cn"

/** Stored is an announcement as the configuration and the store keep it (internal/config/announce.go), outside the contract. */
export type Stored = {
  id: string
  published?: boolean
  title: string
  text?: string
  tone?: Notice["tone"]
  starts?: string
  ends?: string
  place?: string
  link?: { label?: string; url?: string }
  lang?: string
  pages?: ("dashboard" | "present")[]
  audience?: "everyone" | "signed-in"
  from?: string
  until?: string
}

/** Admin is what `GET /api/v1/admin/announcements` answers: the stored ones with their revision, the configured ones, locked. */
export type Admin = { announcements: Stored[]; configured: Stored[]; revision: string; writable: boolean; problem: string }

const path = "/admin/announcements"

export const announcementsQuery = () =>
  queryOptions({
    queryKey: ["admin", "announcements"] as const,
    queryFn: ({ signal }) => get<Admin>(path, signal),
    staleTime: 0,
  })

/** local is an RFC 3339 time as a datetime-local field shows it: its date and clock as written. */
export function local(iso: string | undefined): string {
  const w = iso ? asWritten(iso) : undefined
  if (!w) return ""
  const two = (n: number) => String(n).padStart(2, "0")
  return `${w.year}-${two(w.month)}-${two(w.day)}T${two(w.hour)}:${two(w.minute)}`
}

/** written is a datetime-local field's value as RFC 3339 with the browser's offset on that day: what the admin means by 18:00. */
export function written(value: string): string | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) return undefined
  const offset = -new Date(value).getTimezoneOffset()
  const sign = offset < 0 ? "-" : "+"
  const two = (n: number) => String(Math.floor(Math.abs(n))).padStart(2, "0")
  return `${value}:00${sign}${two(offset / 60)}:${two(offset % 60)}`
}

/** preview is a stored announcement as the pages draw it, without the QR code the server adds. */
export function preview(s: Stored): Notice {
  return {
    id: s.id || "preview",
    revision: "preview",
    title: s.title || "A title",
    text: s.text || undefined,
    tone: s.tone ?? "info",
    startsAt: s.starts,
    endsAt: s.ends,
    place: s.place || undefined,
    link: s.link?.url && s.link.label ? { label: s.link.label, url: s.link.url } : undefined,
    lang: s.lang || undefined,
    until: s.until ?? s.ends ?? "9999-12-31T00:00:00Z",
  }
}

/** state is where an announcement stands at now: a draft, waiting for its from, shown, or over. */
export function state(s: Stored, now: number): "Draft" | "Scheduled" | "Live" | "Ended" {
  if (s.published === false) return "Draft"
  const until = s.until ?? s.ends
  if (until && new Date(until).getTime() <= now) return "Ended"
  if (s.from && new Date(s.from).getTime() > now) return "Scheduled"
  return "Live"
}

/** slug is an id made from a title: "Juleølsmaking 2026" is juleolsmaking-2026. */
export const slug = (title: string) =>
  title
    .toLowerCase()
    .replace(/[æ]/g, "ae")
    .replace(/[ø]/g, "o")
    .replace(/[å]/g, "a")
    .normalize("NFKD")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 63) || "announcement"

const blank = (): Stored => ({
  id: "",
  published: false,
  title: "",
  tone: "event",
  pages: ["dashboard", "present"],
  audience: "everyone",
})

const field = "w-full rounded-md border border-input bg-background px-2 py-1.5 text-sm"

function Field({ label, hint, children }: { label: string; hint?: string; children: (id: string) => ReactNode }) {
  const id = useId()
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-xs font-medium text-muted-foreground">
        {label}
      </label>
      {children(id)}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

/** Editor is one announcement's form beside its preview, drawn as the pages draw it. */
function Editor({
  value,
  taken,
  onChange,
}: {
  value: Stored
  /** the ids other announcements have */
  taken: string[]
  onChange: (s: Stored) => void
}) {
  const now = useNow()
  const set = (patch: Partial<Stored>) => onChange({ ...value, ...patch })
  const time = (key: "starts" | "ends" | "from" | "until") => (id: string) => (
    <input
      id={id}
      type="datetime-local"
      className={field}
      value={local(value[key])}
      onChange={(e) => set({ [key]: written(e.target.value) })}
    />
  )
  const pages = value.pages ?? ["dashboard", "present"]
  const page = (p: "dashboard" | "present", on: boolean) =>
    set({ pages: on ? [...pages.filter((x) => x !== p), p] : pages.filter((x) => x !== p) })
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="sm:col-span-2">
          <Field label="Title" hint="One line, up to 80 characters.">
            {(id) => (
              <input
                id={id}
                className={field}
                maxLength={80}
                value={value.title}
                onChange={(e) => set({ title: e.target.value })}
                required
              />
            )}
          </Field>
        </div>
        <div className="sm:col-span-2">
          <Field label="Text" hint="Optional, one line, up to 280 characters.">
            {(id) => (
              <textarea
                id={id}
                rows={2}
                className={field}
                maxLength={280}
                value={value.text ?? ""}
                onChange={(e) => set({ text: e.target.value })}
              />
            )}
          </Field>
        </div>
        <Field label="Tone">
          {(id) => (
            <select
              id={id}
              className={field}
              value={value.tone ?? "info"}
              onChange={(e) => set({ tone: e.target.value as Notice["tone"] })}
            >
              <option value="event">{tones.event}: violet</option>
              <option value="info">{tones.info}: the portal&apos;s colour</option>
              <option value="notice">{tones.notice}: orange, for maintenance and changes</option>
            </select>
          )}
        </Field>
        <Field label="Language" hint="Of the title and text, such as nb or en.">
          {(id) => (
            <input
              id={id}
              className={field}
              maxLength={35}
              value={value.lang ?? ""}
              onChange={(e) => set({ lang: e.target.value })}
            />
          )}
        </Field>
        <Field label="Starts" hint="Optional: the calendar tile and the countdown.">
          {time("starts")}
        </Field>
        <Field label="Ends" hint="Optional: also when it stops showing.">
          {time("ends")}
        </Field>
        <div className="sm:col-span-2">
          <Field label="Place">
            {(id) => (
              <input
                id={id}
                className={field}
                maxLength={80}
                value={value.place ?? ""}
                onChange={(e) => set({ place: e.target.value })}
              />
            )}
          </Field>
        </div>
        <Field label="Link text">
          {(id) => (
            <input
              id={id}
              className={field}
              maxLength={40}
              value={value.link?.label ?? ""}
              onChange={(e) => set({ link: { ...value.link, label: e.target.value } })}
            />
          )}
        </Field>
        <Field label="Link" hint="https only.">
          {(id) => (
            <input
              id={id}
              type="url"
              className={field}
              placeholder="https://"
              value={value.link?.url ?? ""}
              onChange={(e) => set({ link: { ...value.link, url: e.target.value } })}
            />
          )}
        </Field>
        <Field label="Shown from" hint="Optional: at once.">
          {time("from")}
        </Field>
        <Field label="Shown until" hint="Required unless it ends.">
          {time("until")}
        </Field>
        <fieldset className="flex flex-col gap-1">
          <legend className="text-xs font-medium text-muted-foreground">Shows on</legend>
          {(["dashboard", "present"] as const).map((p) => (
            <label key={p} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="size-4 accent-primary"
                checked={pages.includes(p)}
                disabled={pages.length === 1 && pages.includes(p)}
                onChange={(e) => page(p, e.target.checked)}
              />
              {p === "dashboard" ? "Dashboard" : "Present (/present)"}
            </label>
          ))}
        </fieldset>
        <Field label="Who sees it">
          {(id) => (
            <select
              id={id}
              className={field}
              value={value.audience ?? "everyone"}
              onChange={(e) => set({ audience: e.target.value as Stored["audience"] })}
            >
              <option value="everyone">Everyone</option>
              <option value="signed-in">Signed-in viewers only</option>
            </select>
          )}
        </Field>
        <label className="flex items-center gap-2 text-sm font-medium sm:col-span-2">
          <input
            type="checkbox"
            className="size-4 accent-primary"
            checked={value.published !== false}
            onChange={(e) => set({ published: e.target.checked })}
          />
          Published: off, it is a draft only admins see here
        </label>
        {taken.includes(value.id) && <p className="text-sm text-destructive sm:col-span-2">Another announcement has this id.</p>}
      </div>
      <div className="flex flex-col gap-2">
        <p className="text-xs font-medium tracking-widest text-muted-foreground uppercase">Preview</p>
        <AnnouncementCard a={preview(value)} now={now} onDismiss={() => undefined} />
        <p className="text-xs text-muted-foreground">
          Times are shown as written, in this browser&apos;s offset when saved. On a wall the link also shows as a QR code.
        </p>
      </div>
    </div>
  )
}

/** Row is one announcement in the card's list: its tone, title, where it stands, its pages and audience. */
function Row({ s, now, locked, children }: { s: Stored; now: number; locked?: boolean; children?: ReactNode }) {
  const st = state(s, now)
  return (
    <li className={cn("flex flex-wrap items-center gap-x-3 gap-y-1 py-2", `tone-${s.tone ?? "info"}`)}>
      <span aria-hidden="true" className="size-2.5 shrink-0 rounded-full bg-[var(--tone)]" />
      <span className="min-w-0 flex-1 truncate font-medium">{s.title}</span>
      <span
        className={cn(
          "rounded-md border px-1.5 text-xs",
          st === "Live" ? "border-primary/40 text-primary" : "border-border text-muted-foreground",
        )}
      >
        {st}
      </span>
      <span className="text-xs text-muted-foreground">
        {(s.pages ?? ["dashboard", "present"]).join(", ")}
        {s.audience === "signed-in" ? " · signed-in" : ""}
        {locked ? " · from the configuration" : ""}
      </span>
      {children}
    </li>
  )
}

/**
 * AnnouncementsCard is the admin's announcements (design/announcements.md): the configured ones, locked, and the stored ones to
 * write, edit, publish or hold and remove, each previewed as the pages draw it. Saving sends the whole list with the revision it
 * was read at; someone else's save in between is refused and offered to load.
 */
export function AnnouncementsCard() {
  const qc = useQueryClient()
  const now = useNow()
  const { data, error: loadError, refetch } = useQuery(announcementsQuery())
  const [editing, setEditing] = useState<{ at: number; value: Stored } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ProblemError | null>(null)
  const titleId = useId()
  if (loadError) return <Alert variant="error">The announcements could not be read: {loadError.message}</Alert>
  if (!data) return null
  const taken = (at: number) => [...data.configured, ...data.announcements.filter((_, i) => i !== at)].map((s) => s.id)
  const write = async (list: Stored[]) => {
    setBusy(true)
    setError(null)
    try {
      const next = await send<Admin>("PUT", path, { revision: data.revision, announcements: list })
      qc.setQueryData(announcementsQuery().queryKey, next)
      setEditing(null)
      await Promise.all([
        qc.invalidateQueries({ queryKey: keys.page("dashboard") }),
        qc.invalidateQueries({ queryKey: keys.page("present") }),
      ])
    } catch (e) {
      setError(e instanceof ProblemError ? e : new ProblemError(0, "network", String(e)))
    } finally {
      setBusy(false)
    }
  }
  const save = () => {
    if (!editing) return
    const value = { ...editing.value, id: editing.value.id || slug(editing.value.title) }
    const list = [...data.announcements]
    if (editing.at < 0) list.push(value)
    else list[editing.at] = value
    void write(list)
  }
  const remove = (at: number) => {
    const s = data.announcements[at]
    if (s && window.confirm(`Remove “${s.title}”?`)) void write(data.announcements.filter((_, i) => i !== at))
  }
  const value = editing?.value
  const invalid =
    !value ||
    !value.title.trim() ||
    !(value.until || value.ends) ||
    !!value.link?.url !== !!value.link?.label ||
    taken(editing.at).includes(value.id || slug(value.title))
  return (
    <Card className="lg:col-span-2" aria-labelledby={titleId}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <CardTitle id={titleId}>Announcements</CardTitle>
        {!editing && (
          <Button onClick={() => setEditing({ at: -1, value: blank() })} disabled={!data.writable}>
            New announcement
          </Button>
        )}
      </div>
      <p className="mt-1 text-sm text-muted-foreground">
        Shown at the top of the dashboard and of /present while their time lasts; at most three at once.
      </p>
      {!data.writable && (
        <Alert variant="warn" className="mt-3">
          Saving needs a layout directory: set layout.dir in the configuration. Announcements in scp.yaml show meanwhile.
        </Alert>
      )}
      {data.problem && (
        <Alert variant="error" className="mt-3">
          The stored file is not served: {data.problem}
        </Alert>
      )}
      {error && (
        <Alert variant="error" className="mt-3">
          {error.message}{" "}
          {error.code === "conflict" && (
            <button type="button" className="underline" onClick={() => void refetch().then(() => setError(null))}>
              Load the saved announcements
            </button>
          )}
        </Alert>
      )}
      {data.configured.length + data.announcements.length === 0 && !editing ? (
        <p className="mt-3 text-sm text-muted-foreground">No announcements yet.</p>
      ) : (
        <ul className="mt-3 divide-y divide-border">
          {data.configured.map((s) => (
            <Row key={`c-${s.id}`} s={s} now={now} locked />
          ))}
          {data.announcements.map((s, i) => (
            <Row key={`s-${s.id}`} s={s} now={now}>
              <span className="flex gap-1">
                <Button
                  variant="outline"
                  className="min-h-7 px-2 text-xs"
                  onClick={() => setEditing({ at: i, value: s })}
                  disabled={busy}
                >
                  Edit<span className="sr-only"> {s.title}</span>
                </Button>
                <Button
                  variant="ghost"
                  className="min-h-7 px-2 text-xs"
                  onClick={() => remove(i)}
                  disabled={busy || !data.writable}
                >
                  Remove<span className="sr-only"> {s.title}</span>
                </Button>
              </span>
            </Row>
          ))}
        </ul>
      )}
      {editing && value && (
        <section
          aria-label={editing.at < 0 ? "New announcement" : "Edit announcement"}
          className="mt-4 border-t border-border pt-4"
        >
          <Editor value={value} taken={taken(editing.at)} onChange={(v) => setEditing({ ...editing, value: v })} />
          <div className="mt-4 flex flex-wrap gap-2">
            <Button onClick={save} disabled={invalid || busy || !data.writable}>
              Save
            </Button>
            <Button variant="ghost" onClick={() => setEditing(null)} disabled={busy}>
              Cancel
            </Button>
          </div>
        </section>
      )}
    </Card>
  )
}
