package docs

import "github.com/cantara/source-code-portal/internal/routes"

func routesAbsolute(base, p string) (string, error) { return routes.Absolute(base, p) }
