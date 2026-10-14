import { Link, Outlet, useRouter } from "@tanstack/react-router"
import { useEffect } from "react"
import { StatusBanners } from "@/features/status/banners"
import { apply, useTheme, type Choice } from "@/lib/theme"
import { PanelBoundary } from "@/shared/panel-boundary"

function ThemeSelect() {
  const theme = useTheme()
  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="sr-only sm:not-sr-only">Theme</span>
      <select
        value={theme}
        onChange={(e) => apply(e.target.value as Choice)}
        className="min-h-8 rounded-md border border-input bg-background px-2"
        aria-label="Theme"
      >
        <option value="system">System</option>
        <option value="light">Light</option>
        <option value="dark">Dark</option>
      </select>
    </label>
  )
}

const nav = "rounded-md px-2 py-1 hover:bg-accent [&.active]:font-semibold [&.active]:underline"

/** Root is the shell: skip link, header with navigation and theme, status banners and the page. */
export function Root() {
  const router = useRouter()
  useEffect(
    () =>
      router.subscribe("onResolved", (e) => {
        // After a client navigation the page's h1 takes focus, so screen readers announce the new page.
        if (!e.fromLocation || !e.pathChanged) return
        setTimeout(() => document.querySelector<HTMLElement>("main h1")?.focus(), 0)
      }),
    [router],
  )
  return (
    <>
      <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-background focus:p-2">
        Skip to content
      </a>
      <header className="border-b border-border">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-4 gap-y-2 p-3">
          <Link to="/" className="font-semibold">
            Source Code Portal
          </Link>
          <nav aria-label="Main" className="flex gap-1 text-sm">
            <Link to="/" className={nav} activeOptions={{ exact: true }}>
              Dashboard
            </Link>
            <Link to="/commits" className={nav}>
              Commits
            </Link>
          </nav>
          <div className="ml-auto">
            <ThemeSelect />
          </div>
        </div>
      </header>
      <StatusBanners />
      <main id="main" className="mx-auto max-w-6xl p-4">
        <PanelBoundary>
          <Outlet />
        </PanelBoundary>
      </main>
    </>
  )
}
