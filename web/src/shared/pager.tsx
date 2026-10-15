import type { MouseEvent } from "react"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { count } from "@/lib/format"
import { pageItems, pageSizes } from "@/lib/paging"

type Page = Schemas["Page"]

const cell =
  "inline-flex min-h-9 min-w-9 items-center justify-center rounded-md border border-transparent px-2 text-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"

/**
 * Pager is the numbered pages of a list: previous, the page numbers around the current one, next, and the range shown. Each page
 * is a link (href), so it works without JavaScript; with onPage the page loads in place.
 */
export function Pager({
  page,
  href,
  onPage,
  label = "Pages",
  noun = "commits",
}: {
  page: Page
  href: (n: number) => string
  onPage?: (n: number) => void
  label?: string
  noun?: string
}) {
  const first = page.total === 0 ? 0 : Math.min((page.number - 1) * page.size + 1, page.total)
  const last = Math.min(page.number * page.size, page.total)
  const go = (n: number) => (e: MouseEvent<HTMLAnchorElement>) => {
    if (!onPage || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return
    e.preventDefault()
    onPage(n)
  }
  const link = (n: number, text: string, rel?: string) => (
    <a href={href(n)} onClick={go(n)} rel={rel} className={cn(cell, "hover:bg-accent")}>
      {text}
    </a>
  )
  const dead = (text: string) => (
    <span aria-disabled="true" className={cn(cell, "text-muted-foreground opacity-60")}>
      {text}
    </span>
  )
  return (
    <nav aria-label={label} className="flex flex-wrap items-center justify-between gap-2">
      <p className="text-sm text-muted-foreground" aria-live="polite">
        {first > last ? `No ${noun} on page ${page.number}` : `${count(first)}–${count(last)} of ${count(page.total)} ${noun}`}
      </p>
      {page.pages > 1 && (
        <ul className="flex flex-wrap items-center gap-1">
          <li>{page.number > 1 ? link(Math.min(page.number - 1, page.pages), "Previous", "prev") : dead("Previous")}</li>
          {pageItems(Math.min(page.number, page.pages), page.pages).map((n, i) =>
            n === "gap" ? (
              <li key={`gap${i}`} aria-hidden="true" className={cn(cell, "text-muted-foreground")}>
                …
              </li>
            ) : (
              <li key={n}>
                {n === page.number ? (
                  <a
                    href={href(n)}
                    aria-current="page"
                    aria-label={`Page ${n}`}
                    onClick={go(n)}
                    className={cn(cell, "border-border bg-accent font-semibold")}
                  >
                    {n}
                  </a>
                ) : (
                  <a href={href(n)} aria-label={`Page ${n}`} onClick={go(n)} className={cn(cell, "hover:bg-accent")}>
                    {n}
                  </a>
                )}
              </li>
            ),
          )}
          <li>{page.number < page.pages ? link(page.number + 1, "Next", "next") : dead("Next")}</li>
        </ul>
      )}
    </nav>
  )
}

/** PageSizeField chooses how many items a page shows; a form's field `per`, applied at once with onSize. */
export function PageSizeField({ value, onSize }: { value: number; onSize?: (n: number) => void }) {
  return (
    <label className="flex flex-col">
      Per page
      <select
        name="per"
        value={value}
        onChange={(e) => onSize?.(Number(e.target.value))}
        className="mt-1 min-h-9 rounded-md border border-input bg-background px-2"
      >
        {pageSizes.map((n) => (
          <option key={n} value={n}>
            {n}
          </option>
        ))}
      </select>
    </label>
  )
}
