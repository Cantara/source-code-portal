import { Badge } from "@/components/ui/badge"

/**
 * VisibilityBadge marks a private or internal repository, and a public one an administrator shows to signed-in viewers only (notPublic,
 * Admin › Who sees what); one everyone sees shows nothing.
 */
export function VisibilityBadge({ visibility, notPublic = false }: { visibility: string; notPublic?: boolean }) {
  if (visibility === "private" || visibility === "internal") {
    return <Badge variant="warn">{visibility === "private" ? "Private" : "Internal"}</Badge>
  }
  if (!notPublic) return null
  return (
    <Badge variant="warn" title="Public on GitHub; only signed-in viewers see it here">
      Not public
    </Badge>
  )
}
