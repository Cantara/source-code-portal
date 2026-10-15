import type { ReactNode } from "react"
import type { Crumb } from "@/lib/crumbs"
import { Breadcrumbs } from "./breadcrumbs"

/**
 * PageHeader is the page's one h1 (its title is the page's metadata), under the page's trail when it has one. The shell focuses
 * the h1 after a client navigation.
 */
export function PageHeader({
  title,
  children,
  meta,
  trail,
}: {
  title: string
  children?: ReactNode
  meta?: ReactNode
  trail?: Crumb[]
}) {
  return (
    <header className="mb-6 flex flex-col gap-2">
      {trail && trail.length > 1 && <Breadcrumbs trail={trail} />}
      <div className="flex flex-wrap items-center gap-3">
        <h1 tabIndex={-1} className="text-2xl font-semibold break-words focus:outline-none">
          {title}
        </h1>
        {meta}
      </div>
      {children}
    </header>
  )
}
