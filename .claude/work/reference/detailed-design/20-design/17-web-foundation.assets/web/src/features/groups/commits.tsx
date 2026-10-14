import { Trans } from "@lingui/react/macro"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/groups/$groupId/commits")({
  component: () => (
    <h1>
      <Trans>Group commits</Trans>
    </h1>
  ),
})
