import { Trans } from "@lingui/react/macro"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/repos/$repo/docs/$")({
  component: () => (
    <h1>
      <Trans>Document</Trans>
    </h1>
  ),
})
