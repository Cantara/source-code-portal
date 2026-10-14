import { createFileRoute, redirect } from "@tanstack/react-router"

// Go answers `/` with 302 before the SPA; this covers client-side navigation to "/" and the dev server.
export const Route = createFileRoute("/")({
  beforeLoad: () => {
    throw redirect({ to: "/dashboard", replace: true })
  },
})
