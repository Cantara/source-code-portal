"use client"

import "@/styles/index.css"

/** GlobalError replaces the whole document when the layout itself fails; Try again renders it anew. */
export default function GlobalError({ reset }: { error: Error; reset: () => void }) {
  return (
    <html lang="en">
      <body className="cosmos">
        <main className="p-6">
          <h1 className="text-2xl font-semibold">Source Code Portal is unavailable</h1>
          <p className="mt-2 text-muted-foreground">The page could not be built. The server may be starting or restarting.</p>
          <button type="button" className="mt-4 rounded-md border border-input px-3 py-2 text-sm" onClick={reset}>
            Try again
          </button>
        </main>
      </body>
    </html>
  )
}
