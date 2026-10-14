import { describe, expect, it } from "vitest"
import { allCommitsRoute, groupCommitsRoute, repoCommitsRoute } from "@/features/commits/route"
import { docRoute } from "@/features/docs/route"
import { groupRoute } from "@/features/groups/route"
import { repoRoute } from "@/features/repos/route"
import { api } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"
import { newServerQueryClient } from "@/lib/load"
import type { Ctx } from "@/lib/page"
import { NavigationSignal } from "@/test/navigation"
import { descriptor, http, repo, server } from "@/test/server"
import { panels } from "./repos/[owner]/[name]/panels"

const ctx = <P>(params: P, search = {}): Ctx<P> => ({ api, qc: newServerQueryClient(), params, search })

/** signal is what a load threw, or undefined when it did not. */
async function signal(p: Promise<unknown>): Promise<NavigationSignal | undefined> {
  try {
    await p
  } catch (e) {
    if (e instanceof NavigationSignal) return e
    throw e
  }
  return undefined
}

describe("route modules", () => {
  it("load a repository page with its README and panels into the cache", async () => {
    const c = ctx({ owner: "acme", name: "core" })
    expect(await signal(repoRoute(panels).load(c))).toBeUndefined()
    expect(c.qc.getQueryData(keys.repo("acme", "core"))).toBeDefined()
    expect(c.qc.getQueryData(keys.doc("acme", "core", "README.md"))).toBeDefined()
    expect(c.qc.getQueryData(keys.manifests("acme", "core"))).toBeDefined()
  })

  it("answer an unknown repository or group with the not-found page", async () => {
    expect((await signal(repoRoute(panels).load(ctx({ owner: "acme", name: "nope" }))))?.kind).toBe("not-found")
    expect((await signal(groupRoute.load(ctx({ group: "nope" }))))?.kind).toBe("not-found")
    expect((await signal(groupCommitsRoute.load(ctx({ group: "nope" }))))?.kind).toBe("not-found")
  })

  it("redirect every page of a renamed repository to the same page at its current address", async () => {
    server.use(
      http.get("/repos/{owner}/{name}", ({ response }) =>
        response(200).json({
          repo: repo("Core"),
          title: "",
          descriptor,
          commits: [],
          commitsFreshness: repo("Core").freshness,
        }),
      ),
    )
    const old = { owner: "acme", name: "old-core" }
    expect((await signal(repoRoute(panels).load(ctx(old))))?.to).toBe("/repos/acme/Core")
    expect((await signal(repoCommitsRoute.load(ctx(old))))?.to).toBe("/repos/acme/Core/commits")
    expect((await signal(docRoute.load(ctx({ ...old, path: ["docs", "setup"] }))))?.to).toBe("/repos/acme/Core/docs/docs/setup")
    // Another casing of the current address renders.
    expect(await signal(repoCommitsRoute.load(ctx({ owner: "ACME", name: "CORE" })))).toBeUndefined()
  })

  it("name canonicals after the repository and the group, whatever the request's casing", async () => {
    const lower = { owner: "acme", name: "core" }
    expect((await repoRoute(panels).metadata(ctx(lower))).alternates?.canonical).toBe("/repos/acme/Core")
    expect((await repoCommitsRoute.metadata(ctx(lower))).alternates?.canonical).toBe("/repos/acme/Core/commits")
    expect((await docRoute.metadata(ctx({ ...lower, path: ["docs", "setup"] }))).alternates?.canonical).toBe(
      "/repos/acme/Core/docs/docs/setup",
    )
    expect(await groupCommitsRoute.metadata(ctx({ group: "iam" }))).toMatchObject({ title: "Commits in IAM" })
    expect(await allCommitsRoute.metadata(ctx({}))).toMatchObject({ alternates: { canonical: "/commits" } })
  })
})
