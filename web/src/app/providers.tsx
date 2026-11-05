"use client"

import { QueryClientProvider } from "@tanstack/react-query"
import { useState, type ReactNode } from "react"
import { api } from "@/lib/api/client"
import { noteBuild } from "@/lib/build"
import { NowProvider } from "@/lib/clock"
import { createQueryClient } from "@/lib/query"
import type { Rules } from "@/lib/rules"
import { RulesProvider } from "@/lib/rules-context"

// Every API answer names the server's build; a different one than this bundle's offers a reload.
api.use({
  onResponse({ response }) {
    noteBuild(response.headers.get("X-SCP-Build"))
  },
})

/**
 * Providers holds the browser's query cache, which the pages hydrate from the server, the server's render instant and the rules the
 * server judges by.
 */
export function Providers({ now, rules, children }: { now: number; rules?: Rules; children: ReactNode }) {
  const [qc] = useState(createQueryClient)
  return (
    <NowProvider at={now}>
      <RulesProvider rules={rules}>
        <QueryClientProvider client={qc}>{children}</QueryClientProvider>
      </RulesProvider>
    </NowProvider>
  )
}
