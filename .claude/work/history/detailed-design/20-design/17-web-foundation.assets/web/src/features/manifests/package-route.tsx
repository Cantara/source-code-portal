import { Trans } from "@lingui/react/macro"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/packages")({
  component: () => (
    <h1>
      <Trans>Package</Trans>
    </h1>
  ),
})
