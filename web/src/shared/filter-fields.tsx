"use client"

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api/client"
import { groupsQuery } from "@/lib/api/queries"

/** filterField is the look of a filter form's select or input. */
export const filterField = "mt-1 min-h-9 rounded-md border border-input bg-background px-2"

/** ProductField is the filter forms' "Product" select: every product of the portal, or all. */
export function ProductField({ value }: { value: string | undefined }) {
  const groups = useQuery(groupsQuery(api))
  return (
    <label className="flex flex-col">
      Product
      {/* Uncontrolled: it is made again when the products arrive, so the chosen one is selected whenever they do. */}
      <select key={groups.data ? "ready" : "loading"} name="group" defaultValue={value ?? ""} className={filterField}>
        <option value="">All products</option>
        {groups.data?.groups.map((g) => (
          <option key={g.id} value={g.id}>
            {g.name}
          </option>
        ))}
      </select>
    </label>
  )
}

/** RepoField is the filter forms' "Repository (owner/name)" text. */
export function RepoField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <label className="flex flex-col">
      Repository (owner/name)
      <input name="repo" value={value} onChange={(e) => onChange(e.target.value)} className={filterField} />
    </label>
  )
}
