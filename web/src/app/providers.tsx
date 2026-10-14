"use client"

import { QueryClientProvider } from "@tanstack/react-query"
import { useState, type ReactNode } from "react"
import { api } from "@/lib/api/client"
import { noteBuild } from "@/lib/build"
import { createQueryClient } from "@/lib/query"

// Every API answer names the server's build; a different one than this bundle's offers a reload.
api.use({
  onResponse({ response }) {
    noteBuild(response.headers.get("X-SCP-Build"))
  },
})

/** Providers holds the browser's query cache; the pages hydrate what the server loaded into it. */
export function Providers({ children }: { children: ReactNode }) {
  const [qc] = useState(createQueryClient)
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
}
