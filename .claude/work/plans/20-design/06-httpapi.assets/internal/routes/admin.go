package routes

import "net/http"

// Admin returns a fresh table of the admin listener (:9091).
func Admin() *Table { return newTable("admin", adminRoutes()) }

func adminRoutes() []Route {
	openapi := api("api.openapi", "/api/v1/openapi.yaml", "apispec") // in the admin bundle through the public paths
	retry := api("api.admin.retry", "/api/v1/admin/jobs/retry", "ops")
	retry.Method = http.MethodPost
	return []Route{
		api("api.status", "/api/v1/status", "ops"),
		retry,
		openapi,
		get("spa.status", "/status", ClassSPA, "webapp"),
		get("spa.fallback", "/{path...}", ClassSPA, "webapp"),
		get("admin.metrics", "/metrics", ClassAdmin, "ops"),
		get("admin.pprof", "/debug/pprof/", ClassAdmin, "ops"),
		get("admin.pprof.cmdline", "/debug/pprof/cmdline", ClassAdmin, "ops"),
		get("admin.pprof.profile", "/debug/pprof/profile", ClassAdmin, "ops"),
		get("admin.pprof.symbol", "/debug/pprof/symbol", ClassAdmin, "ops"),
		post("admin.pprof.symbol.post", "/debug/pprof/symbol", ClassAdmin, "ops"),
		get("admin.pprof.trace", "/debug/pprof/trace", ClassAdmin, "ops"),
		get("probe.live", "/livez", ClassServer, "ops"),
		get("probe.ready", "/readyz", ClassServer, "ops"),
		get("assets", "/assets/{file}", ClassServer, "webapp"),
		get("theme.init", "/theme-init.js", ClassServer, "webapp"),
		get("favicon", "/favicon.svg", ClassServer, "webapp"),
		get("manifest", "/manifest.webmanifest", ClassServer, "webapp"),
	}
}
