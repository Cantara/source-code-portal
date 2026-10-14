import { createFileRoute } from "@tanstack/react-router"
export const Route = createFileRoute("/repos/$repo")({ component: () => <div /> })
