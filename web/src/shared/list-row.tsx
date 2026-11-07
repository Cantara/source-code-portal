import type { ReactNode, Ref } from "react"
import { cn } from "@/lib/cn"

/**
 * ListRow is one row of a list page (commits, pull requests, issues, releases, the archive): what leads it at the left (an avatar),
 * the title, and what weighs it at the right of the title's line (aside: the change's size, the comments, when), then the line of
 * what it is (meta). Every list draws its rows through it so their left and right edges are the same. compact keeps a row to two
 * short lines for a side panel or a wall.
 */
export function ListRow({
  lead,
  title,
  aside,
  meta,
  compact = false,
  children,
  rowRef,
  tabIndex,
}: {
  lead?: ReactNode
  title: ReactNode
  aside?: ReactNode
  meta: ReactNode
  compact?: boolean
  /** more under the meta line: a summary, a description */
  children?: ReactNode
  rowRef?: Ref<HTMLLIElement>
  tabIndex?: number
}) {
  return (
    <li ref={rowRef} tabIndex={tabIndex} className={cn("flex gap-3 focus:outline-none", compact ? "py-1.5" : "py-2")}>
      {!compact && lead}
      <div className="min-w-0 flex-1">
        <p className={cn("flex items-baseline gap-2", compact && "text-sm")}>
          {title}
          {!compact && aside && <span className="ml-auto shrink-0 self-center">{aside}</span>}
        </p>
        <p className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-sm text-muted-foreground">{meta}</p>
        {children}
      </div>
    </li>
  )
}

/** Dot is the separator between the facts of a meta line. */
export function Dot() {
  return <span aria-hidden="true">·</span>
}
