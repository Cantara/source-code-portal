import type { Route } from "@/lib/page"
import { pageSize, sizeCookie } from "@/lib/paging"
import { forksCookie, productsView, viewCookie, workCookie, workOf } from "@/lib/preferences"
import { SettingsView } from "./view"

/** settingsRoute is `/settings`: the reader's preferences, read from their cookies so the page opens with them. */
export const settingsRoute: Route<Record<string, never>> = {
  load: async () => {},
  metadata: () => ({ title: "Settings", robots: { index: false, follow: false } }),
  render: ({ cookies }) => (
    <SettingsView
      initial={{
        work: workOf(cookies[workCookie]),
        view: productsView(cookies[viewCookie], "cards"),
        forks: cookies[forksCookie] === "show",
        commits: pageSize(cookies[sizeCookie("commits")], 0),
        history: pageSize(cookies[sizeCookie("history")], 0),
        pulls: pageSize(cookies[sizeCookie("pulls")], 0),
        issues: pageSize(cookies[sizeCookie("issues")], 0),
        releases: pageSize(cookies[sizeCookie("releases")], 0),
        archive: pageSize(cookies[sizeCookie("archive")], 0),
      }}
    />
  ),
}
