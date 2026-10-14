import { Trans } from "@lingui/react/macro"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/repos/$repo/")({
  component: () => (
    <h1>
      <Trans>Repository</Trans>
    </h1>
  ),
})
