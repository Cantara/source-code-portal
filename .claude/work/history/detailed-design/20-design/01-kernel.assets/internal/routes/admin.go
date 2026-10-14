package routes

import "net/http"

// Admin returns a fresh table of the admin listener (:9091).
func Admin() *Table { return newTable("admin", adminRoutes()) }

// adminRoutes: the admin document is a superset of the public one (architecture 11.4: "admin root adds ops admin
// operations"; unit 07 D-1; ADR-010: the SPA bundle serves /status and needs getSite and getSnapshot). Every public API
// operation is therefore also answered on the admin listener by the same handler objects (anonymous principal, public
// view class), and api.openapi there serves the admin bundle. The table's API class is the 22 public operations plus
// api.status and api.admin.retry: 24, equal to the admin document in both directions.
func adminRoutes() []Route {
	retry := api("api.admin.retry", "/api/v1/admin/jobs/retry", "ops")
	retry.Method = http.MethodPost
	rs := []Route{api("api.status", "/api/v1/status", "ops"), retry}
	for _, r := range publicRoutes() {
		if r.Class == ClassAPI {
			rs = append(rs, r) // includes api.openapi
		}
	}
	return append(rs, []Route{
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
	}...)
}
