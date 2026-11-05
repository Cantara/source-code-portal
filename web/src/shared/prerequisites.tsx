"use client"

import { useQuery } from "@tanstack/react-query"
import { Alert } from "@/components/ui/alert"
import { api, type Schemas } from "@/lib/api/client"
import { prerequisitesQuery } from "@/lib/api/queries"

type Prerequisite = Schemas["Prerequisite"]

/** setupDocs is where the operator guide tells what the GitHub App or token needs. */
export const setupDocs = "https://github.com/Cantara/source-code-portal/blob/main/docs/operator-guide.md#1-connect-github"

/**
 * PrerequisiteDetails is what an administrator is told of one unmet prerequisite: the missing permissions and events, where, and
 * the fix; others are told that an administrator can fix it.
 */
export function PrerequisiteDetails({ p }: { p: Prerequisite }) {
  if (!p.fix) return <p>An administrator of the portal can grant what it needs on GitHub.</p>
  return (
    <>
      {(p.permissions?.length ?? 0) > 0 && <p>Missing read permissions: {p.permissions?.join(", ")}.</p>}
      {(p.events?.length ?? 0) > 0 && <p>Missing webhook events: {p.events?.join(", ")}.</p>}
      {(p.owners?.length ?? 0) > 0 && (
        <p>
          Installation{p.owners?.length === 1 ? "" : "s"}: {p.owners?.join(", ")} (source {p.source}).
        </p>
      )}
      <p>{p.fix}</p>
    </>
  )
}

/**
 * PrerequisiteNotice warns on a feature's page when what it needs from GitHub is missing (design/prerequisites.md): what the reader
 * sees without it, and for an administrator the fix and a link to the guide. Nothing when every prerequisite holds.
 */
export function PrerequisiteNotice({ feature }: { feature: Prerequisite["feature"] }) {
  const { data } = useQuery(prerequisitesQuery(api))
  const unmet = data?.unmet.filter((p) => p.feature === feature) ?? []
  if (unmet.length === 0) return null
  return (
    <Alert variant="warn" role="status" aria-label="Disabled feature" className="flex flex-col gap-1">
      <p className="font-semibold">{unmet[0]?.effect}</p>
      {unmet.map((p, i) => (
        <PrerequisiteDetails key={`${p.source ?? ""}-${i}`} p={p} />
      ))}
      {unmet.some((p) => p.fix) && (
        <p>
          <a href={setupDocs} rel="noreferrer" className="underline">
            What the portal needs from GitHub
          </a>
        </p>
      )}
    </Alert>
  )
}
