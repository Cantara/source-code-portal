import { serverGet } from "@/lib/api/server"
import { load } from "@/lib/load"
import type { Route } from "@/lib/page"
import { tokensQuery } from "./api"
import { GitHubView } from "./view"

/** githubRoute is `/github`: the reader's own GitHub access in a portal read with people's tokens (scp enterprise). */
export const githubRoute: Route<Record<string, never>> = {
  // A portal read otherwise answers 404 for the tokens: the page is not found there.
  load: async ({ qc }) => {
    await load(qc, tokensQuery(await serverGet()))
  },
  metadata: () => ({ title: "Your GitHub access", robots: { index: false, follow: false } }),
  render: () => <GitHubView />,
}
