import { createFileRoute, Link } from "@tanstack/react-router"
import { z } from "zod"
export const Route = createFileRoute("/groups/$groupId/commits")({
  validateSearch: z.object({ page: z.number().int().min(1).default(1) }),
  component: () => { const { groupId } = Route.useParams(); const { page } = Route.useSearch(); return <Link to="/repos/$repo" params={{ repo: groupId + page }}>r</Link> },
})
