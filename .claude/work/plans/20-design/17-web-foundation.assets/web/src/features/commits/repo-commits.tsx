import { Trans } from "@lingui/react/macro"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/repos/$repo/commits")({
  component: () => (
    <h1>
      <Trans>Repository commits</Trans>
    </h1>
  ),
})
