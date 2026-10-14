import { Trans } from "@lingui/react/macro"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/groups/$groupId/")({
  component: () => (
    <h1>
      <Trans>Group</Trans>
    </h1>
  ),
})
