import { Trans } from "@lingui/react/macro"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/status")({
  component: () => (
    <h1>
      <Trans>Status</Trans>
    </h1>
  ),
})
