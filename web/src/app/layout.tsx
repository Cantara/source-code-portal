import type { Metadata, Viewport } from "next"
import Link from "next/link"
import type { ReactNode } from "react"
import { StatusBanners } from "@/features/status/banners"
import { origin, privateViewer } from "@/lib/api/server"
import { Providers } from "./providers"
import { FocusOnNavigation, MainNav, ThemeSelect } from "./shell"
import "@/styles/index.css"
import "@/styles/docs.css"

export async function generateMetadata(): Promise<Metadata> {
  return {
    metadataBase: new URL(await origin()),
    title: { template: "%s · Source Code Portal", default: "Source Code Portal" },
    description: "Repositories, documentation and commits of the organisation.",
    icons: { icon: { url: "/favicon.svg", type: "image/svg+xml" } },
    // A private viewer's pages show private repositories: no search engine may keep them.
    ...((await privateViewer()) ? { robots: { index: false, follow: false } } : {}),
  }
}

export const viewport: Viewport = { colorScheme: "light dark", width: "device-width", initialScale: 1 }

/** RootLayout is the shell: skip link, header with navigation and theme, status banners and the page. */
export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        {/* Sets the stored theme before the first paint. */}
        <script src="/theme-init.js" />
      </head>
      <body>
        <Providers>
          <FocusOnNavigation />
          <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-background focus:p-2">
            Skip to content
          </a>
          <header className="border-b border-border">
            <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-4 gap-y-2 p-3">
              <Link href="/" className="font-semibold">
                Source Code Portal
              </Link>
              <MainNav />
              <div className="ml-auto">
                <ThemeSelect />
              </div>
            </div>
          </header>
          <StatusBanners />
          <main id="main" className="mx-auto max-w-6xl p-4">
            {children}
          </main>
        </Providers>
      </body>
    </html>
  )
}
