"use client"

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useId, useState, type ReactNode } from "react"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { ProblemError, send, type Schemas } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { layoutPath, storedLayoutQuery, type Page, type StoredLayout } from "./api"
import { blank, kindLabel, kinds, type Element, type Kind } from "./layout"
import { useFeatures } from "@/lib/rules-context"

type Group = Schemas["DashboardGroup"]

const same = (a: Element[], b: Element[]) => JSON.stringify(a) === JSON.stringify(b)

/** move is list with the item at from moved to to. */
function move<T>(list: T[], from: number, to: number): T[] {
  if (to < 0 || to >= list.length || from === to) return list
  const out = [...list]
  const [item] = out.splice(from, 1)
  out.splice(to, 0, item as T)
  return out
}

/** Options are an element's settings: which products, how they are drawn, how many commits, the heading's text. */
function Options({ el, groups, onChange }: { el: Element; groups: Group[]; onChange: (el: Element) => void }) {
  const id = useId()
  const chosen = el.groups ?? []
  const field = "rounded-md border border-input bg-background px-2 py-1 text-sm"
  return (
    <div className="flex flex-col gap-2 text-sm">
      {(el.kind === "products" || el.kind === "fleet-map") && (
        <fieldset className="flex flex-col gap-1">
          <legend className="text-xs text-muted-foreground">Products</legend>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              name={`${id}-groups`}
              checked={!el.groups}
              onChange={() => onChange({ ...el, groups: undefined })}
            />
            Every product
          </label>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              name={`${id}-groups`}
              checked={!!el.groups}
              onChange={() => onChange({ ...el, groups: groups.slice(0, 1).map((g) => g.id) })}
            />
            Chosen products
          </label>
          {el.groups && (
            <ul className="ml-6 flex max-h-48 flex-col gap-0.5 overflow-y-auto">
              {groups.map((g) => (
                <li key={g.id}>
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={chosen.includes(g.id)}
                      onChange={(e) => {
                        const next = e.target.checked
                          ? groups.filter((x) => x.id === g.id || chosen.includes(x.id)).map((x) => x.id)
                          : chosen.filter((x) => x !== g.id)
                        if (next.length > 0) onChange({ ...el, groups: next })
                      }}
                    />
                    <span className="truncate">{g.name}</span>
                  </label>
                </li>
              ))}
            </ul>
          )}
        </fieldset>
      )}
      {el.kind === "products" && (
        <label className="flex items-center justify-between gap-2">
          <span className="text-xs text-muted-foreground">Show as</span>
          <select
            className={field}
            value={el.view ?? "cards"}
            onChange={(e) => onChange({ ...el, view: e.target.value as Element["view"] })}
          >
            <option value="cards">Cards</option>
            <option value="table">Table</option>
            <option value="tiles">Tiles</option>
          </select>
        </label>
      )}
      {el.kind === "fleet-map" && (
        <label className="flex items-center justify-between gap-2">
          <span className="text-xs text-muted-foreground">Colour by</span>
          <select
            className={field}
            value={el.color ?? "activity"}
            onChange={(e) => onChange({ ...el, color: e.target.value as Element["color"] })}
          >
            <option value="activity">Last change</option>
            <option value="health">Health</option>
          </select>
        </label>
      )}
      {el.kind === "latest-commits" && (
        <label className="flex items-center justify-between gap-2">
          <span className="text-xs text-muted-foreground">Commits</span>
          <input
            type="number"
            min={1}
            max={50}
            className={cn(field, "w-20")}
            value={el.count ?? 10}
            onChange={(e) => onChange({ ...el, count: Math.min(50, Math.max(1, Number(e.target.value) || 1)) })}
          />
        </label>
      )}
      {el.kind === "heading" && (
        <>
          <label className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">Heading</span>
            <input
              className={field}
              maxLength={120}
              value={el.text ?? ""}
              onChange={(e) => onChange({ ...el, text: e.target.value })}
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">Line under it (optional)</span>
            <input
              className={field}
              maxLength={500}
              value={el.note ?? ""}
              onChange={(e) => onChange({ ...el, note: e.target.value || undefined })}
            />
          </label>
        </>
      )}
    </div>
  )
}

/**
 * Designer is the design mode of a page with a layout (the landing page, the presentation page), for admins: the page's elements in a
 * list to add, move (buttons, or by dragging), set up and remove, beside a live preview of the draft, drawn as the page draws it.
 * Save keeps it in the layout store for every viewer; a save that another admin's overtook is refused, never overwritten.
 */
export function Designer({
  page,
  groups,
  preview,
  onClose,
}: {
  page: Page
  groups: Group[]
  preview: (draft: Element[]) => ReactNode
  onClose: () => void
}) {
  const qc = useQueryClient()
  const { data: stored, error: loadError, refetch } = useQuery(storedLayoutQuery(page))
  const [edited, setDraft] = useState<Element[] | null>(null)
  const [adding, setAdding] = useState<Kind>("heading")
  const features = useFeatures()
  const offered = kinds.filter((k) => (k.kind !== "pulls" || features.pulls) && (k.kind !== "issues" || features.issues))
  const [dragging, setDragging] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const addId = useId()
  const [error, setError] = useState<ProblemError | null>(null)
  const [saved, setSaved] = useState(false) // the last write was a save, not a return to the built-in layout
  if (loadError) return <Alert variant="error">The layout could not be read: {loadError.message}</Alert>
  if (!stored) return <p className="text-sm text-muted-foreground">Opening the design mode…</p>
  const draft = edited ?? stored.elements
  const dirty = !same(draft, stored.elements)
  const invalid = draft.length === 0 || draft.some((el) => el.kind === "heading" && !el.text?.trim())
  const update = (i: number, el: Element) => setDraft(draft.map((x, j) => (j === i ? el : x)))
  const write = async (w: () => Promise<StoredLayout>) => {
    setBusy(true)
    setError(null)
    try {
      const next = await w()
      qc.setQueryData(storedLayoutQuery(page).queryKey, next)
      setDraft(next.elements)
      setSaved(next.stored)
      await qc.invalidateQueries({ queryKey: keys.page(page) })
    } catch (e) {
      setError(e instanceof ProblemError ? e : new ProblemError(0, "network", String(e)))
    } finally {
      setBusy(false)
    }
  }
  const save = () => write(() => send<StoredLayout>("PUT", layoutPath(page), { revision: stored.revision, elements: draft }))
  const reset = () =>
    write(() => send<StoredLayout>("DELETE", `${layoutPath(page)}?revision=${encodeURIComponent(stored.revision)}`))
  const reload = async () => {
    const r = await refetch()
    if (r.data) setDraft(r.data.elements)
    setError(null)
  }
  const close = () => {
    if (!dirty || window.confirm("Leave the design mode and drop the unsaved changes?")) onClose()
  }
  return (
    <div className="flex flex-col gap-4">
      <section
        aria-label="Design mode"
        className="panel sticky top-16 z-20 flex flex-wrap items-center justify-between gap-3 border-primary/40 p-3"
      >
        <div className="flex flex-col">
          <h2 className="text-sm font-semibold">Design mode</h2>
          <p className="text-xs text-muted-foreground" aria-live="polite">
            {dirty
              ? "Unsaved changes"
              : saved
                ? "Saved: every viewer sees this layout"
                : stored.stored
                  ? "The stored layout"
                  : "The built-in layout, for every viewer"}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button onClick={save} disabled={!dirty || invalid || busy || !stored.writable}>
            Save
          </Button>
          <Button variant="outline" onClick={() => setDraft(stored.elements)} disabled={!dirty || busy}>
            Discard changes
          </Button>
          {stored.stored && (
            <Button variant="outline" onClick={reset} disabled={busy || !stored.writable}>
              Back to the built-in layout
            </Button>
          )}
          <Button variant="ghost" onClick={close}>
            Done
          </Button>
        </div>
      </section>
      {!stored.writable && (
        <Alert variant="warn">
          Saving needs a layout directory: set layout.dir in the configuration. You can try layouts here meanwhile.
        </Alert>
      )}
      {stored.problem && <Alert variant="error">The stored file is not served: {stored.problem}</Alert>}
      {error && (
        <Alert variant="error">
          {error.message}{" "}
          {error.code === "conflict" && (
            <button type="button" className="underline" onClick={reload}>
              Load the saved layout
            </button>
          )}
        </Alert>
      )}
      <div className="grid gap-6 xl:grid-cols-[22rem_minmax(0,1fr)]">
        <section aria-labelledby="elements" className="panel flex flex-col gap-3 self-start p-3 xl:sticky xl:top-40">
          <h2 id="elements" className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
            Elements
          </h2>
          <ol className="flex flex-col gap-2">
            {draft.map((el, i) => (
              <li
                key={i}
                draggable
                onDragStart={() => setDragging(i)}
                onDragOver={(e) => e.preventDefault()}
                onDrop={() => {
                  if (dragging !== null) setDraft(move(draft, dragging, i))
                  setDragging(null)
                }}
                onDragEnd={() => setDragging(null)}
                className={cn("rounded-md border border-border p-2", dragging === i && "opacity-50")}
              >
                <div className="mb-2 flex items-center gap-2">
                  <span aria-hidden="true" className="cursor-grab text-muted-foreground select-none" title="Drag to move">
                    ⠿
                  </span>
                  <span className="flex-1 text-sm font-medium">{kindLabel(el.kind)}</span>
                  <Button
                    variant="ghost"
                    className="min-h-7 px-1.5"
                    aria-label={`Move ${kindLabel(el.kind)} up`}
                    disabled={i === 0}
                    onClick={() => setDraft(move(draft, i, i - 1))}
                  >
                    ↑
                  </Button>
                  <Button
                    variant="ghost"
                    className="min-h-7 px-1.5"
                    aria-label={`Move ${kindLabel(el.kind)} down`}
                    disabled={i === draft.length - 1}
                    onClick={() => setDraft(move(draft, i, i + 1))}
                  >
                    ↓
                  </Button>
                  <Button
                    variant="ghost"
                    className="min-h-7 px-1.5"
                    aria-label={`Remove ${kindLabel(el.kind)}`}
                    disabled={draft.length === 1}
                    onClick={() => setDraft(draft.filter((_, j) => j !== i))}
                  >
                    ✕
                  </Button>
                </div>
                <Options el={el} groups={groups} onChange={(next) => update(i, next)} />
              </li>
            ))}
          </ol>
          <div className="flex flex-col gap-2 border-t border-border/60 pt-3">
            <div className="flex items-center gap-2 text-sm">
              <label htmlFor={`${addId}-kind`} className="text-xs text-muted-foreground">
                New element
              </label>
              <select
                id={`${addId}-kind`}
                className="flex-1 rounded-md border border-input bg-background px-2 py-1 text-sm"
                value={adding}
                onChange={(e) => setAdding(e.target.value as Kind)}
              >
                {offered.map((k) => (
                  <option key={k.kind} value={k.kind}>
                    {k.label}
                  </option>
                ))}
              </select>
              <Button
                variant="outline"
                className="min-h-8"
                onClick={() => setDraft([...draft, blank(adding)])}
                disabled={draft.length >= 50}
              >
                Add
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">{kinds.find((k) => k.kind === adding)?.about}</p>
          </div>
        </section>
        <section aria-label="Preview" className="min-w-0 rounded-lg border border-dashed border-border p-3">
          {preview(draft)}
        </section>
      </div>
    </div>
  )
}
