"use client"

import { useRouter } from "next/navigation"
import { useId, useState, type ReactNode } from "react"
import { Card, CardTitle } from "@/components/ui/card"
import { pageSizes, sizeCookie, type PagedList } from "@/lib/paging"
import { forget, kinds, productsViews, remember, viewCookie, workCookie, type ProductsView, type Work } from "@/lib/preferences"
import { apply, useTheme, type Choice } from "@/lib/theme"
import { PageHeader } from "@/shared/page-header"

type Settings = { work: Work; view: ProductsView; commits: number; history: number; pulls: number }

/** Choices is one setting as a group of radio buttons, each with what it means. */
function Choices<T extends string>({
  legend,
  value,
  options,
  onChange,
}: {
  legend: string
  value: T
  options: { value: T; label: string; about?: string }[]
  onChange: (v: T) => void
}) {
  const name = useId()
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-1 text-sm font-medium">{legend}</legend>
      {options.map((o) => (
        <label key={o.value} className="flex items-start gap-2 text-sm">
          <input
            type="radio"
            name={name}
            value={o.value}
            checked={value === o.value}
            onChange={() => onChange(o.value)}
            className="mt-1 accent-primary"
          />
          <span>
            {o.label}
            {o.about && <span className="block text-xs text-muted-foreground">{o.about}</span>}
          </span>
        </label>
      ))}
    </fieldset>
  )
}

/** Kinds is the work that counts as checkboxes, one per kind; the last one ticked stays, so something always counts. */
function Kinds({ value, onChange }: { value: Work; onChange: (w: Work) => void }) {
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-1 text-sm font-medium">Count, at least one</legend>
      {kinds.map((k) => {
        const on = value.includes(k.value)
        return (
          <label key={k.value} className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              value={k.value}
              checked={on}
              disabled={on && value.length === 1}
              onChange={() => onChange(kinds.map((x) => x.value).filter((x) => (x === k.value ? !on : value.includes(x))))}
              className="mt-1 accent-primary"
            />
            <span>
              {k.label}
              <span className="block text-xs text-muted-foreground">{k.about}</span>
            </span>
          </label>
        )
      })}
    </fieldset>
  )
}

function Section({ title, about, children }: { title: string; about: string; children: ReactNode }) {
  const id = useId()
  return (
    <Card aria-labelledby={id} className="flex flex-col gap-3">
      <div>
        <CardTitle id={id}>{title}</CardTitle>
        <p className="text-sm text-muted-foreground">{about}</p>
      </div>
      {children}
    </Card>
  )
}

/**
 * SettingsView is the reader's preferences: the work that counts, how products are shown, the page sizes and the theme. Each choice
 * is kept at once in this browser's cookies (the theme in its storage) and the pages follow it; nothing is stored on the server.
 */
export function SettingsView({ initial }: { initial: Settings }) {
  const router = useRouter()
  const theme = useTheme()
  const [s, setS] = useState(initial)
  const [saved, setSaved] = useState("")
  const keep = (next: Partial<Settings>, said: string) => {
    setS({ ...s, ...next })
    setSaved(said)
    router.refresh()
  }
  const size = (list: PagedList, label: string) => (
    <label className="flex items-center justify-between gap-3 text-sm">
      {label}
      <select
        className="min-h-8 rounded-md border border-border bg-card px-2 text-sm"
        value={s[list]}
        onChange={(e) => {
          const n = Number(e.target.value)
          if (n === 0) forget(sizeCookie(list))
          else remember(sizeCookie(list), String(n))
          keep({ [list]: n }, `${label}: ${n === 0 ? "the default" : n}`)
        }}
      >
        <option value={0}>Default</option>
        {pageSizes.map((n) => (
          <option key={n} value={n}>
            {n}
          </option>
        ))}
      </select>
    </label>
  )
  return (
    <>
      <PageHeader title="Settings" meta={<span className="text-xs text-muted-foreground">Kept in this browser</span>} />
      <p className="sr-only" aria-live="polite">
        {saved && `Saved. ${saved}.`}
      </p>
      <div className="grid max-w-5xl gap-4 md:grid-cols-2">
        <Section
          title="Work that counts"
          about="People and agents work together, and automation maintains. Choose what the dashboard counts: its numbers, sparklines, people, last change and fleet map. The other kinds stay in sight beside it."
        >
          <Kinds
            value={s.work}
            onChange={(work) => {
              remember(workCookie, work.join(","))
              keep(
                { work },
                `Work that counts: ${kinds
                  .filter((k) => work.includes(k.value))
                  .map((k) => k.label.toLowerCase())
                  .join(", ")}`,
              )
            }}
          />
        </Section>
        <Section title="Products" about="How the dashboard shows the products.">
          <Choices
            legend="Show as"
            value={s.view}
            options={productsViews.map((v) => ({ value: v, label: v === "cards" ? "Cards" : "Table" }))}
            onChange={(view) => {
              remember(viewCookie, view)
              keep({ view }, `Products: ${view}`)
            }}
          />
        </Section>
        <Section title="Page sizes" about="How many commits or pull requests a page of a list shows.">
          {size("commits", "Commit timelines")}
          {size("history", "Repository histories")}
          {size("pulls", "Pull requests")}
        </Section>
        <Section title="Theme" about="As the system is set, dark or light.">
          <Choices
            legend="Theme"
            value={theme}
            options={[
              { value: "system" as Choice, label: "System" },
              { value: "dark" as Choice, label: "Dark" },
              { value: "light" as Choice, label: "Light" },
            ]}
            onChange={(t) => {
              apply(t)
              setSaved(`Theme: ${t}`)
            }}
          />
        </Section>
      </div>
    </>
  )
}
