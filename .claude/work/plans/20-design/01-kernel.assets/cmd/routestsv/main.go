// Command routestsv prints the public and admin route tables as TSV (the generator of 01-kernel.routes.tsv and of the
// route reference of docs/reference). It is a development tool; the product does not ship it.
package main

import (
	"fmt"
	"os"

	"github.com/cantara/source-code-portal/internal/routes"
)

func main() {
	w := os.Stdout
	fmt.Fprintln(w, "listener\tname\tmethod\tpattern\tclass\towner")
	for _, l := range []struct {
		name string
		t    *routes.Table
	}{{"public", routes.Public()}, {"admin", routes.Admin()}} {
		for _, r := range l.t.All() {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", l.name, r.Name, r.Method, r.Pattern, r.Class, r.Owner)
		}
	}
}
