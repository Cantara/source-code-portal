import { Trans } from "@lingui/react/macro"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/repos/$repo/dependencies")({
  component: () => (
    <h1>
      <Trans>Dependencies</Trans>
    </h1>
  ),
})
