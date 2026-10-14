import { Trans } from "@lingui/react/macro"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/dependencies")({
  component: () => (
    <h1>
      <Trans>Org dependencies</Trans>
    </h1>
  ),
})
