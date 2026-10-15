import type { Metadata, Viewport } from "next"
import Link from "next/link"
import type { ReactNode } from "react"
import { LiveIndicator, StatusBanners } from "@/features/status/banners"
import { unwrap } from "@/lib/api/client"
import { origin, privateViewer, renderedAt, serverApi } from "@/lib/api/server"
import { Providers } from "./providers"
import { FocusOnNavigation, MainNav, Mark, ThemeSelect, ViewerMenu } from "./shell"
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

/** release is the portal's release as the header shows it: v2.0.1, or dev for a local build. */
function release(): string {
  const v = process.env.SCP_VERSION ?? "dev"
  return /^\d/.test(v) ? `v${v}` : v
}

/** RootLayout is the shell: skip link, header with navigation, the viewer and theme, status banners and the page. */
export default async function RootLayout({ children }: { children: ReactNode }) {
  // Without the API the header leaves the viewer out; the page shows the problem.
  const server = await serverApi()
  const [me, assistant] = await Promise.all([
    unwrap(server.GET("/me")).catch(() => undefined),
    unwrap(server.GET("/assistant")).catch(() => undefined),
  ])
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        {/* Sets the stored theme before the first paint. */}
        <script src="/theme-init.js" />
      </head>
      <body className="cosmos">
        <Providers now={renderedAt()}>
          <FocusOnNavigation />
          <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-background focus:p-2">
            Skip to content
          </a>
          <header className="sticky top-0 z-40 border-b border-border bg-background/75 backdrop-blur-md">
            <div className="flex flex-wrap items-center gap-x-6 gap-y-2 px-4 py-2.5 lg:px-6">
              <Link href="/" className="group flex items-center gap-2.5 font-semibold tracking-tight">
                <Mark />
                <span>
                  Source Code Portal
                  <span className="ml-2 hidden font-mono text-[0.6875rem] font-normal tracking-widest text-muted-foreground uppercase sm:inline">
                    Mission control
                  </span>
                  <span className="ml-2 font-mono text-[0.6875rem] font-normal text-muted-foreground" title="Release">
                    {release()}
                  </span>
                </span>
              </Link>
              <MainNav ask={!!assistant?.enabled && assistant.allowed} />
              <div className="ml-auto flex items-center gap-4">
                <LiveIndicator />
                <ViewerMenu me={me} />
                <ThemeSelect />
              </div>
            </div>
          </header>
          <StatusBanners />
          <main id="main" className="px-4 py-5 lg:px-6">
            {children}
          </main>
        </Providers>
      </body>
    </html>
  )
}
