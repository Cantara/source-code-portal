import { rootRoute, route, index } from "@tanstack/virtual-file-routes"

export const routes = rootRoute("app/root.tsx", [
  index("app/index-redirect.tsx"),
  route("/dashboard", "features/dashboard/route.tsx"),
  route("/groups/$groupId", "features/groups/route.tsx", [
    index("features/groups/overview.tsx"),
    route("/commits", "features/groups/commits.tsx"),
  ]),
  route("/commits", "features/commits/route.tsx"),
  route("/repos/$repo", "features/docs/route.tsx", [
    index("features/docs/readme.tsx"),
    route("/docs/$", "features/docs/doc.tsx"),
    route("/commits", "features/commits/repo-commits.tsx"),
    route("/dependencies", "features/manifests/repo-dependencies.tsx"),
  ]),
  route("/dependencies", "features/manifests/dependencies-route.tsx"),
  route("/packages", "features/manifests/package-route.tsx"),
  route("/pages/$slug", "features/pages/route.tsx"),
  route("/status", "features/status/route.tsx"),
])

export const adminOnly: readonly string[] = ["/status"]
