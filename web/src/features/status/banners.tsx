"use client"

import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { useNewVersion } from "@/lib/build"
import { useConnectionFailing } from "@/lib/connection"
import { useSnapshotSync } from "./sync"

/** StatusBanners keeps the snapshot in sync and says when the portal is warming, unreachable or has a new version. */
export function StatusBanners() {
  const snapshot = useSnapshotSync()
  const failing = useConnectionFailing()
  const newer = useNewVersion()
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-2 px-4 empty:hidden">
      <div role="status" aria-live="polite" className="contents">
        {failing && (
          <Alert variant="warn" className="mt-3">
            Cannot refresh right now. Showing the last data that loaded.
          </Alert>
        )}
        {snapshot && !snapshot.ready && (
          <Alert variant="warn" className="mt-3">
            The portal is loading its catalogue from GitHub. Pages fill in as it arrives.
          </Alert>
        )}
      </div>
      {newer && (
        <Alert className="mt-3 flex flex-wrap items-center justify-between gap-2">
          <span>A new version is available.</span>
          <Button variant="outline" onClick={() => window.location.reload()}>
            Reload
          </Button>
        </Alert>
      )}
    </div>
  )
}
