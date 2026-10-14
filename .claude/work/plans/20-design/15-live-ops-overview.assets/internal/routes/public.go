package routes

import "net/http"

func api(name, pattern, owner string) Route {
	return Route{Name: name, Method: http.MethodGet, Pattern: pattern, Class: ClassAPI, Owner: owner}
}
func get(name, pattern string, c Class, owner string) Route {
	return Route{Name: name, Method: http.MethodGet, Pattern: pattern, Class: c, Owner: owner}
}
func post(name, pattern string, c Class, owner string) Route {
	return Route{Name: name, Method: http.MethodPost, Pattern: pattern, Class: c, Owner: owner}
}

// Public returns a fresh table of the public listener (:9090).
func Public() *Table { return newTable("public", publicRoutes()) }

func publicRoutes() []Route {
	openapi := api("api.openapi", "/api/v1/openapi.yaml", "apispec") // operation getOpenApiDocument, tag meta; served by httpapi.Static
	return []Route{
		// ---- API (JSON; registered through Table.Mux; patterns are the OpenAPI paths) ----
		api("api.site", "/api/v1/site", "overview"),
		openapi,
		api("api.snapshot", "/api/v1/snapshot", "live"),
		api("api.events", "/api/v1/events", "live"), // SSE, v1.1 behind live.sse.enabled
		api("api.groups", "/api/v1/groups", "catalog"),
		api("api.group", "/api/v1/groups/{groupId}", "catalog"),
		api("api.group.repos", "/api/v1/groups/{groupId}/repos", "catalog"),
		api("api.repos", "/api/v1/repos", "catalog"),
		api("api.repo", "/api/v1/repos/{repo}", "overview"),
		api("api.commits", "/api/v1/commits", "commits"),
		api("api.docs.list", "/api/v1/repos/{repo}/docs", "docs"),
		api("api.docs.get", "/api/v1/repos/{repo}/docs/{docId}", "docs"),
		api("api.manifests.repo", "/api/v1/repos/{repo}/manifests", "manifests"),
		api("api.manifests.deps", "/api/v1/repos/{repo}/dependencies", "manifests"),
		api("api.manifests.sbom", "/api/v1/repos/{repo}/sbom.cdx.json", "manifests"),
		api("api.manifests.graph", "/api/v1/dependencies/graph", "manifests"),
		api("api.manifests.versions", "/api/v1/dependencies/versions", "manifests"),
		api("api.manifests.licenses", "/api/v1/dependencies/licenses", "manifests"),
		api("api.manifests.component", "/api/v1/components", "manifests"),
		api("api.manifests.dependents", "/api/v1/components/dependents", "manifests"),
		api("api.pages", "/api/v1/pages", "pages"),
		api("api.page", "/api/v1/pages/{slug}", "pages"),

		// ---- SPA page shapes (answered with the shell; negotiated ones also serve Markdown) ----
		get("spa.root", "/{$}", ClassSPA, "webapp"), // 302 to /dashboard
		get("spa.dashboard", "/dashboard", ClassSPA, "webapp"),
		get("negotiated.group", "/groups/{groupId}", ClassSPA, "overview"),
		get("spa.group.commits", "/groups/{groupId}/commits", ClassSPA, "webapp"),
		get("spa.commits", "/commits", ClassSPA, "webapp"),
		get("negotiated.repo", "/repos/{repo}", ClassSPA, "overview"),
		get("doc.md", "/repos/{repo}/docs/{path...}", ClassSPA, "docs"), // a .md/.adoc suffix serves raw text, otherwise negotiated
		get("spa.repo.commits", "/repos/{repo}/commits", ClassSPA, "webapp"),
		get("spa.repo.dependencies", "/repos/{repo}/dependencies", ClassSPA, "webapp"),
		get("spa.dependencies", "/dependencies", ClassSPA, "webapp"),
		get("spa.packages", "/packages", ClassSPA, "webapp"), // ?purl=
		get("spa.page", "/pages/{slug}", ClassSPA, "webapp"),
		get("spa.fallback", "/{path...}", ClassSPA, "webapp"), // history fallback; never for reserved prefixes and suffixes

		// ---- server-only ----
		get("badge", "/badges/{repo}/{file}", ClassServer, "badges"),
		get("badge.group", "/badges/groups/{groupId}/{file}", ClassServer, "badges"),
		get("media.repo", "/media/{repo}/{ref}/{path...}", ClassServer, "media"),
		get("media.ext", "/media/ext/{sig}/{id}", ClassServer, "media"),
		get("readme.md", "/repos/{repo}/readme.md", ClassServer, "docs"),
		get("agents.md", "/repos/{repo}/agents.md", ClassServer, "docs"),
		get("changelog.md", "/repos/{repo}/changelog.md", ClassServer, "docs"),
		get("negotiated.repo.rest", "/repos/{repo}/{rest...}", ClassServer, "overview"), // alias redirect, else the shell's real 404
		get("llms.index", "/llms.txt", ClassServer, "overview"),
		get("llms.full", "/llms-full.txt", ClassServer, "overview"),
		get("llms.group", "/groups/{groupId}/llms.txt", ClassServer, "overview"),
		get("llms.group.full", "/groups/{groupId}/llms-full.txt", ClassServer, "overview"),
		get("llms.repo", "/repos/{repo}/llms.txt", ClassServer, "overview"),
		get("llms.repo.full", "/repos/{repo}/llms-full.txt", ClassServer, "overview"),
		get("deps.group.md", "/groups/{groupId}/dependencies.md", ClassServer, "manifests"),
		get("deps.repo.md", "/repos/{repo}/dependencies.md", ClassServer, "manifests"),
		get("sitemap", "/sitemap.xml", ClassServer, "overview"),
		get("robots", "/robots.txt", ClassServer, "overview"),
		get("schemas", "/schemas/scp/{file}", ClassServer, "docs"),
		post("webhook", "/webhooks/github", ClassServer, "webhook"),
		post("webhook.source", "/webhooks/github/{source}", ClassServer, "webhook"),
		post("csp.report", "/csp-reports", ClassServer, "webapp"),
		get("probe.live", "/livez", ClassServer, "ops"),
		get("probe.ready", "/readyz", ClassServer, "ops"),
		get("assets", "/assets/{file}", ClassServer, "webapp"),
		get("theme.init", "/theme-init.js", ClassServer, "webapp"),
		get("favicon", "/favicon.svg", ClassServer, "webapp"),
		get("manifest", "/manifest.webmanifest", ClassServer, "webapp"),
		get("frame.mermaid", "/_frame/mermaid.html", ClassServer, "webapp"), // v1.1
	}
}
