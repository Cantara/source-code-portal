"use client"

import { useQuery } from "@tanstack/react-query"
import { Card, CardTitle } from "@/components/ui/card"
import { ecosystemLabel } from "@/entities/repo/packages"
import { api } from "@/lib/api/client"
import { repoQuery } from "@/lib/api/queries"
import { RelativeTime } from "@/shared/relative-time"

/** PackagesCard is what the repository publishes to package registries, each with its newest release there. */
export function PackagesCard({ owner, name }: { owner: string; name: string }) {
  const { data } = useQuery(repoQuery(api, owner, name))
  const packages = data?.repo.packages ?? []
  if (packages.length === 0) return null
  return (
    <Card aria-labelledby="packages">
      <CardTitle id="packages">Packages</CardTitle>
      <ul className="mt-2 flex flex-col gap-3 text-sm">
        {packages.map((p) => (
          <li key={`${p.ecosystem} ${p.name}`} className="flex min-w-0 flex-col gap-0.5">
            <span className="flex flex-wrap items-baseline gap-x-2">
              <span className="font-mono text-xs break-all">{p.name}</span>
              <span className="text-muted-foreground">{ecosystemLabel(p.ecosystem)}</span>
            </span>
            <span className="flex flex-wrap items-baseline gap-x-2 text-muted-foreground">
              {p.url ? (
                <a href={p.url} rel="noreferrer nofollow" className="font-medium text-foreground hover:underline">
                  {p.version}
                </a>
              ) : (
                <span className="font-medium text-foreground">{p.version}</span>
              )}
              <span>at {p.registry}</span>
              {p.publishedAt && <RelativeTime at={p.publishedAt} />}
            </span>
          </li>
        ))}
      </ul>
    </Card>
  )
}
