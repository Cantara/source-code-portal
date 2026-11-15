"use client"

import { cn } from "@/lib/cn"
import { audiences, differs, origin, type Audience, type Effective, type Setting } from "./visibility-model"

/**
 * ItemRow is one product or repository in who sees what: its selection, its three-way choice (everyone refused for one private on
 * GitHub), the search-engine switch of what everyone sees, and where its choice comes from with the way back to scp.yaml or to
 * inheriting (design/visibility.md, design/config-baseline.md).
 */
export function ItemRow({
  name,
  effective,
  setting,
  file,
  why,
  product,
  locked,
  meta,
  selected,
  changed,
  disabled,
  onSelect,
  onChoose,
  onSearch,
  onReset,
  onInherit,
}: {
  name: string
  effective: Effective
  /** its own choice; null when it takes its audience from elsewhere */
  setting: Setting | null
  /** scp.yaml's choice for it */
  file: Setting | undefined
  /** where its audience comes from, in words */
  why: string
  product: boolean
  /** private on GitHub: never shown to everyone */
  locked: boolean
  meta: string
  selected: boolean
  changed: boolean
  disabled: boolean
  onSelect: (on: boolean) => void
  onChoose: (audience: Audience) => void
  onSearch: (on: boolean) => void
  onReset: () => void
  onInherit: () => void
}) {
  return (
    <li
      className={cn(
        "grid grid-cols-[2rem_minmax(0,1fr)] items-center gap-x-2 gap-y-1 border-t border-border py-2 md:grid-cols-[2rem_minmax(0,1fr)_auto_11rem_12rem]",
        product && "bg-muted/40",
        selected && "bg-accent/40",
      )}
    >
      <span className="flex justify-center">
        <input type="checkbox" aria-label={`Select ${name}`} checked={selected} onChange={(ev) => onSelect(ev.target.checked)} />
      </span>
      <span className={cn("flex min-w-0 flex-wrap items-center gap-2", !product && "pl-4")}>
        <span className={product ? "text-sm font-semibold" : "font-mono text-xs break-all"}>{name}</span>
        {meta && <span className="text-xs text-muted-foreground">{meta}</span>}
        {changed && (
          <span className="rounded-full bg-amber-100 px-2 text-xs font-medium text-amber-950 dark:bg-amber-950 dark:text-amber-100">
            changed
          </span>
        )}
      </span>
      <span
        role="radiogroup"
        aria-label={`Who sees ${name}`}
        className="col-start-2 inline-flex w-fit overflow-hidden rounded-md border border-border md:col-start-auto"
      >
        {audiences.map((a) => {
          const on = effective.audience === a.value
          const capped = locked && a.value === "everyone"
          return (
            <button
              key={a.value}
              type="button"
              role="radio"
              aria-checked={on}
              disabled={disabled || capped}
              title={capped ? "Private on GitHub: never shown to everyone" : undefined}
              onClick={() => onChoose(a.value)}
              className={cn(
                "min-h-9 border-r border-border px-3 text-xs last:border-r-0 disabled:cursor-not-allowed",
                on ? cn(a.on, "font-semibold") : "text-muted-foreground hover:bg-accent",
                capped && "line-through opacity-60",
              )}
            >
              {a.label}
            </button>
          )
        })}
      </span>
      <span className="col-start-2 flex items-center gap-2 text-xs md:col-start-auto">
        {effective.audience === "everyone" ? (
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              role="switch"
              checked={effective.search}
              disabled={disabled}
              onChange={(ev) => onSearch(ev.target.checked)}
            />
            {effective.search ? "offered to search engines" : "kept from search engines"}
          </label>
        ) : (
          <span className="text-muted-foreground">never offered to search engines</span>
        )}
      </span>
      <span className="col-start-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground md:col-start-auto">
        {origin(setting, file, why)}
        {differs(setting, file) && file && (
          <button type="button" className="underline" disabled={disabled} onClick={onReset}>
            reset to scp.yaml
          </button>
        )}
        {setting && !file && (
          <button type="button" className="underline" disabled={disabled} onClick={onInherit}>
            {product ? "use the default" : "inherit"}
          </button>
        )}
      </span>
    </li>
  )
}
