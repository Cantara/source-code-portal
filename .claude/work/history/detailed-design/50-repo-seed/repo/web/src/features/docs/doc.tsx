import { createFileRoute } from "@tanstack/react-router"
import { Trans } from "@lingui/react/macro"

// Seed stub: replaced by the real page of design 18 (docs).
export const Route = createFileRoute("/repos/$repo/docs/$")({
  component: () => (
    <section>
      <h1 className="text-2xl font-semibold">
        <Trans>Document</Trans>
      </h1>
      <p>
        <Trans>Not implemented yet.</Trans>
      </p>
    </section>
  ),
})
