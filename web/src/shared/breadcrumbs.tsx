import Link from "next/link"
import type { Crumb } from "@/lib/crumbs"

/** Breadcrumbs draws a page's trail above its h1: the pages above as links, the page itself marked current. */
export function Breadcrumbs({ trail }: { trail: Crumb[] }) {
  return (
    <nav aria-label="Breadcrumb" className="text-sm text-muted-foreground">
      <ol className="flex flex-wrap items-center gap-1">
        {trail.map((c, i) => (
          <li key={c.path} className="flex items-center gap-1">
            {i > 0 && <span aria-hidden="true">/</span>}
            {i < trail.length - 1 ? (
              <Link href={c.path} className="hover:text-foreground hover:underline">
                {c.name}
              </Link>
            ) : (
              <span aria-current="page" className="break-all">
                {c.name}
              </span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  )
}
