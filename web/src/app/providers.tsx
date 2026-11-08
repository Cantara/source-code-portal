"use client"

import { QueryClientProvider } from "@tanstack/react-query"
import { useEffect, useState, type ReactNode } from "react"
import { api } from "@/lib/api/client"
import { noteBuild } from "@/lib/build"
import { NowProvider } from "@/lib/clock"
import { createQueryClient } from "@/lib/query"
import type { Rules } from "@/lib/rules"
import { RulesProvider } from "@/lib/rules-context"
import type { Look } from "@/lib/look"
import { LookProvider } from "@/lib/look-context"

// Every API answer names the server's build; a different one than this bundle's offers a reload.
api.use({
  onResponse({ response }) {
    noteBuild(response.headers.get("X-SCP-Build"))
  },
})

/**
 * Providers holds the browser's query cache, which the pages hydrate from the server, the server's render instant, the rules the
 * server judges by and the deployment's look.
 */
export function Providers({ now, rules, look, children }: { now: number; rules?: Rules; look?: Look; children: ReactNode }) {
  const [qc] = useState(createQueryClient)
  // The root's effects run after its children's: once this one has, the page answers to clicks. The end-to-end tests wait for it
  // instead of pressing again until something happens.
  useEffect(() => {
    document.documentElement.dataset.hydrated = ""
  }, [])
  return (
    <NowProvider at={now}>
      <RulesProvider rules={rules}>
        <LookProvider look={look}>
          <QueryClientProvider client={qc}>{children}</QueryClientProvider>
        </LookProvider>
      </RulesProvider>
    </NowProvider>
  )
}
