"use client"

import { queryOptions, useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect, useId, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardTitle } from "@/components/ui/card"
import { get, ProblemError, send } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { plural } from "@/lib/format"
import {
  audiences,
  changes,
  clone,
  describe,
  matches,
  sameRules,
  warnAt,
  whyOf,
  type Audience,
  type Change,
  type Doc,
  type Effective,
  type Filter,
  type Item,
  type Rule,
  type Setting,
  type Visibility,
} from "./visibility-model"
import { ItemRow } from "./visibility-row"

const path = "/admin/visibility"

export const visibilityQuery = () =>
  queryOptions({
    queryKey: ["admin", "visibility"] as const,
    queryFn: ({ signal }) => get<Visibility>(path, signal),
    staleTime: 0,
  })

/**
 * VisibilityCard is who sees what (design/visibility.md): every product and repository with its audience (everyone, signed in, no
 * one), the search-engine switch of what everyone sees, where each comes from, and the rules for repositories that arrive later.
 * Changes are a draft the portal previews; saving shows them first, and a save can be undone.
 */
export function VisibilityCard() {
  const qc = useQueryClient()
  const { data, error: loadError, refetch } = useQuery(visibilityQuery())
  const [draft, setDraft] = useState<Doc | null>(null)
  const [preview, setPreview] = useState<Visibility | null>(null)
  const [problem, setProblem] = useState("")
  const [filter, setFilter] = useState("")
  const [show, setShow] = useState<Filter>("all")
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [reviewing, setReviewing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ProblemError | null>(null)
  const [undo, setUndo] = useState<{ doc: Doc; count: number } | null>(null)
  const titleId = useId()
  const filterId = useId()
  const revision = data?.revision
  useEffect(() => {
    if (!draft) return
    const ctl = new AbortController()
    const t = setTimeout(() => {
      send<Visibility>("POST", path + "/preview", draft)
        .then((p) => (ctl.signal.aborted ? undefined : (setPreview(p), setProblem(""))))
        .catch((e: unknown) => (ctl.signal.aborted ? undefined : setProblem(e instanceof Error ? e.message : String(e))))
    }, 250)
    return () => {
      clearTimeout(t)
      ctl.abort()
    }
  }, [draft, revision])
  if (loadError) return <Alert variant="error">Who sees what could not be read: {loadError.message}</Alert>
  if (!data) return null
  const doc: Doc = draft ?? { groups: data.groups, repos: data.repos, rules: data.rules }
  const view = draft ? (preview ?? data) : data
  const pending = draft && preview ? changes(data, preview) : []
  const changed = new Set(pending.map((c) => c.key))
  const edit = (fn: (d: Doc) => void) => {
    const next = clone(doc)
    fn(next)
    setDraft(next)
    setUndo(null)
  }
  const reset = () => (setDraft(null), setPreview(null), setProblem(""), setReviewing(false))
  const choose = (key: string, audience: Audience, current: Effective) =>
    edit((d) => {
      const [kind, id] = [key.slice(0, 1), key.slice(2)]
      const search = audience === "everyone" && current.audience === "everyone" ? current.search : true
      ;(kind === "g" ? d.groups : d.repos)[id] = { audience, search }
    })
  const searchOn = (key: string, on: boolean) =>
    edit((d) => {
      const [kind, id] = [key.slice(0, 1), key.slice(2)]
      ;(kind === "g" ? d.groups : d.repos)[id] = { audience: "everyone", search: on }
    })
  const inherit = (key: string) =>
    edit((d) => {
      const [kind, id] = [key.slice(0, 1), key.slice(2)]
      if (kind === "g") d.groups = Object.fromEntries(Object.entries(d.groups).filter(([k]) => k !== id))
      else d.repos = Object.fromEntries(Object.entries(d.repos).filter(([k]) => k !== id))
    })
  const configured = data.configured ?? { groups: {}, repos: {}, rules: [] }
  const fileOf = (key: string): Setting | undefined => (key.startsWith("g:") ? configured.groups : configured.repos)[key.slice(2)]
  const toFile = (key: string) => {
    const st = fileOf(key)
    if (!st) return inherit(key)
    edit((d) => {
      ;(key.startsWith("g:") ? d.groups : d.repos)[key.slice(2)] = { ...st }
    })
  }
  const effectiveOf = (key: string): (Effective & { private?: boolean }) | undefined => {
    if (key.startsWith("g:")) return view.tree.find((g) => g.id === key.slice(2))
    const a = key.slice(2)
    return [...view.tree.flatMap((g) => g.repos), ...view.ungrouped].find((r) => r.address === a)
  }
  const bulkAudience = (audience: Audience) => {
    const next = clone(doc)
    for (const key of selected) {
      const e = effectiveOf(key)
      if (!e || (audience === "everyone" && e.private)) continue
      const [kind, id] = [key.slice(0, 1), key.slice(2)]
      ;(kind === "g" ? next.groups : next.repos)[id] = {
        audience,
        search: audience === "everyone" && e.audience === "everyone" ? e.search : true,
      }
    }
    setDraft(next)
    setUndo(null)
  }
  const bulkSearch = (on: boolean) => {
    const next = clone(doc)
    for (const key of selected) {
      const e = effectiveOf(key)
      if (!e || e.audience !== "everyone") continue
      const [kind, id] = [key.slice(0, 1), key.slice(2)]
      ;(kind === "g" ? next.groups : next.repos)[id] = { audience: "everyone", search: on }
    }
    setDraft(next)
    setUndo(null)
  }
  const save = async (body: Doc, rev: string) => {
    setBusy(true)
    setError(null)
    try {
      const before: Doc = { groups: data.groups, repos: data.repos, rules: data.rules }
      const count = pending.length
      const next = await send<Visibility>("PUT", path, { ...body, revision: rev })
      qc.setQueryData(visibilityQuery().queryKey, next)
      reset()
      setSelected(new Set())
      return { before, count }
    } catch (e) {
      setError(e instanceof ProblemError ? e : new ProblemError(0, "network", String(e)))
      return null
    } finally {
      setBusy(false)
    }
  }
  const confirm = async () => {
    const done = await save(doc, data.revision)
    if (done) setUndo({ doc: done.before, count: done.count })
  }
  const undoSave = async () => {
    if (!undo) return
    const latest = qc.getQueryData<Visibility>(visibilityQuery().queryKey) ?? data
    await save(undo.doc, latest.revision)
    setUndo(null)
  }

  const passes = (r: Item) => {
    if (!matches(r.address, filter)) return false
    switch (show) {
      case "everyone":
      case "signed-in":
      case "no-one":
        return r.audience === show
      case "private":
        return r.private
      case "search-off":
        return r.audience === "everyone" && !r.search
      case "changed":
        return changed.has("r:" + r.address)
      default:
        return true
    }
  }
  const every = new Map([...view.tree.flatMap((g) => g.repos), ...view.ungrouped].map((r) => [r.address, r]))
  const total = every.size
  const chips: { value: Filter; label: string; count: number }[] = [
    { value: "all", label: "All", count: total },
    { value: "everyone", label: "Everyone", count: view.counts.everyone },
    { value: "signed-in", label: "Signed in", count: view.counts.signedIn },
    { value: "no-one", label: "No one", count: view.counts.noOne },
    { value: "private", label: "Private on GitHub", count: view.counts.private },
    { value: "search-off", label: "Kept from search engines", count: view.counts.searchOff },
    ...(pending.length > 0 ? [{ value: "changed" as const, label: "Changed", count: pending.length }] : []),
  ]
  const toggle = (key: string, on: boolean) => {
    const next = new Set(selected)
    if (on) next.add(key)
    else next.delete(key)
    setSelected(next)
  }
  const disabled = !data.writable || busy
  const row = (
    key: string,
    name: string,
    e: Effective,
    setting: Setting | null,
    product: boolean,
    priv: boolean,
    meta: string,
  ) => (
    <ItemRow
      key={key}
      name={name}
      effective={e}
      setting={setting}
      file={fileOf(key)}
      why={whyOf(e, doc.rules)}
      product={product}
      locked={priv}
      meta={meta}
      selected={selected.has(key)}
      changed={changed.has(key)}
      disabled={disabled}
      onSelect={(on) => toggle(key, on)}
      onChoose={(audience) => choose(key, audience, e)}
      onSearch={(on) => searchOn(key, on)}
      onReset={() => toFile(key)}
      onInherit={() => inherit(key)}
    />
  )
  const products = view.tree
    .map((g) => ({ g, repos: g.repos.filter(passes) }))
    .filter(({ g, repos }) => repos.length > 0 || (show === "all" && matches(g.name, filter)))
  const ungrouped = view.ungrouped.filter(passes)
  const back = pending.filter((c) => c.back).length

  return (
    <Card className="lg:col-span-2" aria-labelledby={titleId}>
      <CardTitle id={titleId}>Who sees what</CardTitle>
      <p className="mt-1 text-sm text-muted-foreground">
        Choose who sees each product and repository in the portal: everyone, only signed-in viewers, or no one. What everyone sees
        can also be kept from search engines such as Google. A repository private on GitHub is never shown to everyone. The portal
        keeps reading every repository from GitHub, webhook events too, whatever you choose here.
      </p>
      {!data.writable && (
        <Alert variant="warn" className="mt-3">
          This portal has no store to save the choices in.
        </Alert>
      )}
      {data.problem && (
        <Alert variant="error" className="mt-3">
          The stored choices are not served: {data.problem}
        </Alert>
      )}
      {problem && (
        <Alert variant="error" className="mt-3">
          {problem}
        </Alert>
      )}
      {error && (
        <Alert variant="error" className="mt-3">
          {error.message}{" "}
          {error.code === "conflict" && (
            <button type="button" className="underline" onClick={() => void refetch().then(() => (reset(), setError(null)))}>
              Load the saved choices
            </button>
          )}
        </Alert>
      )}
      {undo && (
        <Alert className="mt-3" role="status">
          Saved {plural(undo.count, "change", "changes")}.{" "}
          <button type="button" className="underline" disabled={busy} onClick={() => void undoSave()}>
            Undo
          </button>
        </Alert>
      )}

      <dl className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[
          ["Everyone", view.counts.everyone],
          ["Signed in only", view.counts.signedIn],
          ["No one", view.counts.noOne],
          ["Kept from search engines", view.counts.searchOff],
        ].map(([label, n]) => (
          <div key={label} className="rounded-md border border-border p-3">
            <dd className="text-2xl font-semibold">{n}</dd>
            <dt className="text-xs text-muted-foreground">{label}</dt>
          </div>
        ))}
      </dl>

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <label htmlFor={filterId} className="sr-only">
          Find a product or repository
        </label>
        <input
          id={filterId}
          type="search"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Find a product or repository"
          className="min-h-9 w-full max-w-sm rounded-md border border-border bg-background px-2 text-sm"
        />
        <div role="group" aria-label="Show" className="flex flex-wrap gap-1">
          {chips.map((c) => (
            <button
              key={c.value}
              type="button"
              aria-pressed={show === c.value}
              onClick={() => setShow(c.value)}
              className={cn(
                "min-h-9 rounded-full border px-3 text-xs",
                show === c.value ? "border-primary bg-primary text-primary-foreground" : "border-border hover:bg-accent",
              )}
            >
              {c.label} {c.count}
            </button>
          ))}
        </div>
      </div>

      {selected.size > 0 && (
        <div
          role="group"
          aria-label="Selected"
          className="mt-3 flex flex-wrap items-center gap-2 rounded-md border border-border p-2 text-sm"
        >
          <span className="font-medium">{selected.size} selected:</span>
          {audiences.map((a) => (
            <Button key={a.value} variant="outline" disabled={disabled} onClick={() => bulkAudience(a.value)}>
              {a.label}
            </Button>
          ))}
          <Button variant="outline" disabled={disabled} onClick={() => bulkSearch(false)}>
            Keep from search engines
          </Button>
          <Button variant="outline" disabled={disabled} onClick={() => bulkSearch(true)}>
            Offer to search engines
          </Button>
          <Button variant="ghost" onClick={() => setSelected(new Set())}>
            Clear selection
          </Button>
        </div>
      )}

      <ul aria-label="Products and repositories" className="mt-3">
        {products.length === 0 && ungrouped.length === 0 && (
          <li className="py-2 text-sm text-muted-foreground">Nothing matches.</li>
        )}
        {products.map(({ g, repos }) => [
          row("g:" + g.id, g.name, g, g.setting, true, false, plural(g.repos.length, "repository", "repositories")),
          ...repos.map((r) =>
            row(
              "r:" + r.address,
              r.address,
              r,
              r.setting,
              false,
              r.private,
              [r.private ? "private" : "", r.fork ? "fork" : ""].filter(Boolean).join(" · "),
            ),
          ),
        ])}
        {ungrouped.length > 0 && <li className="border-t border-border pt-3 pb-1 text-sm font-medium">In no product</li>}
        {ungrouped.map((r) =>
          row(
            "r:" + r.address,
            r.address,
            r,
            r.setting,
            false,
            r.private,
            [r.private ? "private" : "", r.fork ? "fork" : ""].filter(Boolean).join(" · "),
          ),
        )}
      </ul>

      {view.missing.length > 0 && (
        <div className="mt-3 text-sm">
          <p className="font-medium">Chosen, but not in the catalogue now (renamed, deleted or not listed yet)</p>
          <ul className="mt-1">
            {view.missing.map((a) => (
              <li key={a} className="flex flex-wrap items-center gap-2 py-1">
                <span className="font-mono text-xs">{a}</span>
                <span className="text-xs text-muted-foreground">
                  {describe(doc.repos[a] ?? { audience: "everyone", search: true })}
                </span>
                <button type="button" className="text-xs underline" disabled={disabled} onClick={() => inherit("r:" + a)}>
                  forget
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}

      <Rules doc={doc} configured={configured.rules} matches={view.ruleMatches} disabled={disabled} edit={edit} />

      {draft && (
        <div className="sticky bottom-0 z-10 mt-4 flex flex-wrap items-center gap-2 border-t border-border bg-background py-3">
          <span className="text-sm">
            <strong>{plural(pending.length, "unsaved change", "unsaved changes")}</strong>
          </span>
          <span className="ml-auto flex gap-2">
            <Button variant="ghost" onClick={reset} disabled={busy}>
              Discard
            </Button>
            <Button onClick={() => setReviewing(true)} disabled={disabled || problem !== "" || pending.length === 0}>
              Review and save
            </Button>
          </span>
        </div>
      )}
      {reviewing && (
        <Review changes={pending} back={back} busy={busy} onBack={() => setReviewing(false)} onSave={() => void confirm()} />
      )}
    </Card>
  )
}

/** Rules are the patterns that set the audience of the repositories whose address they match, now and when one arrives. */
function Rules({
  doc,
  configured,
  matches: counts,
  disabled,
  edit,
}: {
  doc: Doc
  configured: Rule[]
  matches: number[]
  disabled: boolean
  edit: (fn: (d: Doc) => void) => void
}) {
  const id = useId()
  const choice = (r: Setting) => (r.audience === "everyone" ? (r.search ? "everyone" : "everyone-off") : r.audience)
  const fromChoice = (v: string): Setting =>
    v === "everyone-off" ? { audience: "everyone", search: false } : { audience: v as Audience, search: true }
  return (
    <details className="mt-4 rounded-md border border-border p-3" open={doc.rules.length > 0}>
      <summary className="cursor-pointer text-sm font-medium">
        Rules for repositories that arrive later{doc.rules.length > 0 ? ` · ${doc.rules.length}` : ""}
      </summary>
      {!sameRules(doc.rules, configured) && (
        <p className="mt-2 flex flex-wrap items-center gap-2 text-xs">
          <span className="rounded-full bg-muted px-2 font-medium">changed here</span>
          <span className="text-muted-foreground">
            scp.yaml has {configured.length === 0 ? "no rules" : plural(configured.length, "rule", "rules")}.
          </span>
          <button
            type="button"
            className="underline"
            disabled={disabled}
            onClick={() => edit((d) => (d.rules = configured.map((r) => ({ ...r }))))}
          >
            reset to scp.yaml
          </button>
        </p>
      )}
      <p className="mt-2 text-xs text-muted-foreground">
        A rule sets who sees every repository whose address (owner/name) matches, now and the day a new one arrives. Regular
        expressions, not case-sensitive; the first rule that matches wins. A choice made on a repository wins over a rule, and a
        rule over its products.
      </p>
      <ol className="mt-2 flex flex-col gap-2">
        {doc.rules.map((r, i) => (
          <li key={i} className="flex flex-wrap items-center gap-2 text-sm">
            <label htmlFor={`${id}-p${i}`} className="sr-only">
              Rule {i + 1}: address matches
            </label>
            <input
              id={`${id}-p${i}`}
              value={r.pattern}
              disabled={disabled}
              spellCheck={false}
              placeholder="^acme/legacy-"
              onChange={(e) => edit((d) => void (d.rules[i] = { ...r, pattern: e.target.value }))}
              className="min-h-9 w-full max-w-xs rounded-md border border-border bg-background px-2 font-mono text-xs"
            />
            <label htmlFor={`${id}-a${i}`} className="sr-only">
              Rule {i + 1}: who sees the repositories it matches
            </label>
            <select
              id={`${id}-a${i}`}
              value={choice(r)}
              disabled={disabled}
              onChange={(e) => edit((d) => void (d.rules[i] = { ...r, ...fromChoice(e.target.value) }))}
              className="min-h-9 rounded-md border border-border bg-background px-2 text-sm"
            >
              <option value="everyone">Everyone, offered to search engines</option>
              <option value="everyone-off">Everyone, kept from search engines</option>
              <option value="signed-in">Signed in only</option>
              <option value="no-one">No one</option>
            </select>
            <span className="text-xs text-muted-foreground">
              matches {plural(counts[i] ?? 0, "repository", "repositories")} today
            </span>
            <Button variant="ghost" disabled={disabled} onClick={() => edit((d) => void d.rules.splice(i, 1))}>
              Remove
            </Button>
          </li>
        ))}
      </ol>
      <Button
        variant="outline"
        className="mt-2"
        disabled={disabled}
        onClick={() => edit((d) => void d.rules.push({ pattern: "", audience: "signed-in", search: true }))}
      >
        Add a rule
      </Button>
    </details>
  )
}

/** section is the review's heading for a change. */
function section(c: Change): string {
  if (c.to.audience === "no-one") return "Hidden from everyone"
  if (c.to.audience === "signed-in")
    return c.from.audience === "everyone" ? "Leave the public view" : "Back for signed-in viewers"
  return c.from.audience === "everyone" ? "Search engines" : "Back in the public view"
}

/** Review lists what a save changes, grouped by what it does, and warns in words when many items come back into public view. */
function Review({
  changes: list,
  back,
  busy,
  onBack,
  onSave,
}: {
  changes: Change[]
  back: number
  busy: boolean
  onBack: () => void
  onSave: () => void
}) {
  const titleId = useId()
  const heading = useRef<HTMLHeadingElement>(null)
  useEffect(() => heading.current?.focus(), [])
  const titles = [
    "Hidden from everyone",
    "Leave the public view",
    "Back in the public view",
    "Back for signed-in viewers",
    "Search engines",
  ]
  const groups = titles
    .map((title) => ({ title, items: list.filter((c) => section(c) === title) }))
    .filter((g) => g.items.length > 0)
  // In the body, so no card's stacking context puts the page's sticky header above it.
  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/60 p-4"
      onKeyDown={(e) => e.key === "Escape" && onBack()}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="mt-8 w-full max-w-xl rounded-lg border border-border bg-background p-5 shadow-xl"
      >
        <h2 id={titleId} ref={heading} tabIndex={-1} className="text-lg font-semibold outline-none">
          Review {plural(list.length, "change", "changes")}
        </h2>
        <p className="text-sm text-muted-foreground">Nothing changes on the portal until you save.</p>
        {back >= warnAt && (
          <Alert variant="warn" className="mt-3">
            {back} items come back into the public view or the sitemap.
          </Alert>
        )}
        {groups.map((g) => (
          <section key={g.title} className="mt-4">
            <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
              {g.title} · {g.items.length}
            </h3>
            <ul className="mt-2 flex flex-col gap-2">
              {g.items.map((c) => (
                <li key={c.key} className="rounded-md border border-border p-3 text-sm">
                  <span className={c.product ? "font-semibold" : "font-mono text-xs"}>
                    {c.product ? `${c.name} (product)` : c.name}
                  </span>
                  <span className="block">
                    {describe(c.from)} → <strong>{describe(c.to)}</strong>
                  </span>
                  <span className="block text-xs text-muted-foreground">{c.effect}</span>
                </li>
              ))}
            </ul>
          </section>
        ))}
        <div className="mt-5 flex flex-wrap justify-end gap-2">
          <Button variant="outline" onClick={onBack} disabled={busy}>
            Back to editing
          </Button>
          <Button onClick={onSave} disabled={busy}>
            Save {plural(list.length, "change", "changes")}
          </Button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
