import { Trans } from "@lingui/react/macro"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/commits")({
  component: () => (
    <h1>
      <Trans>Commits</Trans>
    </h1>
  ),
})
