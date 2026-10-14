import { Badge } from "@/components/ui/badge"

/** VisibilityBadge marks a private or internal repository; public ones show nothing. */
export function VisibilityBadge({ visibility }: { visibility: string }) {
  if (visibility !== "private" && visibility !== "internal") return null
  return <Badge variant="warn">{visibility === "private" ? "Private" : "Internal"}</Badge>
}
