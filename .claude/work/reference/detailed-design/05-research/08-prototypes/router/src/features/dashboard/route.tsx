import { createFileRoute, Link } from "@tanstack/react-router"
import { z } from "zod"
export const Route = createFileRoute("/dashboard")({
  validateSearch: z.object({ q: z.string().optional(), sort: z.enum(["name", "updated"]).default("name") }),
  component: () => <Link to="/groups/$groupId" params={{ groupId: "identity" }} search={{ page: 1 }}>g</Link>,
})
