import { rootRoute, route, index, layout } from "@tanstack/virtual-file-routes"
export const routes = rootRoute("app/root.tsx", [
  index("app/index-redirect.tsx"),
  route("/dashboard", "features/dashboard/route.tsx"),
  route("/groups/$groupId", "features/groups/route.tsx", [
    index("features/groups/overview.tsx"),
    route("/commits", "features/groups/commits.tsx"),
  ]),
  route("/repos/$repo", "features/repos/route.tsx"),
])
