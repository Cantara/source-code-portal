import { createFileRoute } from "@tanstack/react-router"
import { Trans } from "@lingui/react/macro"

// Seed stub: replaced by the real page of design 18 (manifests).
export const Route = createFileRoute("/dependencies")({
  component: () => (
    <section>
      <h1 className="text-2xl font-semibold">
        <Trans>Dependencies</Trans>
      </h1>
      <p>
        <Trans>Not implemented yet.</Trans>
      </p>
    </section>
  ),
})
