import { useEffect, type ReactNode } from "react"

/** PageHeader is the page's one h1; it sets the document title. The shell focuses it after a client navigation. */
export function PageHeader({ title, children, meta }: { title: string; children?: ReactNode; meta?: ReactNode }) {
  useEffect(() => {
    document.title = `${title} · Source Code Portal`
  }, [title])
  return (
    <header className="mb-6 flex flex-col gap-2">
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
