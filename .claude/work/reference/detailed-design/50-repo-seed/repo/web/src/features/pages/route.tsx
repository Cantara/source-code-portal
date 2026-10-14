import { createFileRoute } from "@tanstack/react-router"
import { Trans } from "@lingui/react/macro"

// Seed stub: replaced by the real page of design 18 (pages).
export const Route = createFileRoute("/pages/$slug")({
  component: () => (
    <section>
      <h1 className="text-2xl font-semibold">
        <Trans>Page</Trans>
      </h1>
      <p>
        <Trans>Not implemented yet.</Trans>
      </p>
    </section>
  ),
})
