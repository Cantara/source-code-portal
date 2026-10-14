import { Trans } from "@lingui/react/macro"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { useServerStatus } from "@/lib/server-status"

/** One polite live region for the three states that change what the user can trust. Ticking text is never announced. */
export function ConnectionBanner() {
  const s = useServerStatus()
  return (
    <div role="status" aria-live="polite" data-testid="connection-banner">
      {s.buildSkew ? (
        <Alert>
          <AlertDescription>
            <Trans>A new version of the portal is available.</Trans>{" "}
            <Button size="sm" variant="outline" onClick={() => window.location.reload()}>
              <Trans>Reload</Trans>
            </Button>
          </AlertDescription>
        </Alert>
      ) : !s.reachable ? (
        <Alert>
          <AlertDescription>
            <Trans>Cannot refresh right now. Showing the last data that loaded.</Trans>
          </AlertDescription>
        </Alert>
      ) : s.readiness === "warming" ? (
        <Alert>
          <AlertDescription>
            <Trans>The portal is still loading data from GitHub. Some views may be incomplete.</Trans>
          </AlertDescription>
        </Alert>
      ) : null}
    </div>
  )
}
