import { plural } from "@lingui/core/macro"
import { Trans } from "@lingui/react/macro"
import type { components } from "@/lib/api/schema"
import { RelativeTime } from "./relative-time"

type Site = components["schemas"]["Site"]

/** "Updated" is the last full sync (never render time, GR-05) and the count is repositories, not groups. */
export function HeaderStatus({ site }: { site: Pick<Site, "lastFullSync" | "repoCount"> }) {
  return (
    <p className="text-xs text-muted-foreground">
      {plural(site.repoCount, { one: "# repository", other: "# repositories" })}
      {" \u00b7 "}
      {site.lastFullSync ? (
        <Trans>
          Updated <RelativeTime iso={site.lastFullSync} />
        </Trans>
      ) : (
        <Trans>Not synced yet</Trans>
      )}
    </p>
  )
}
