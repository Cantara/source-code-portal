package main

import (
	"fmt"
	"os"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/cantara/source-code-portal/internal/httpapi/apitest"
)

func main() {
	l := openapi3.NewLoader()
	l.IsExternalRefsAllowed = true
	doc, err := l.LoadFromFile(os.Args[1])
	if err != nil {
		fmt.Println("load:", err)
		os.Exit(1)
	}
	if err := doc.Validate(l.Context); err != nil {
		fmt.Println("validate:", err)
	}
	v := apitest.Lint(doc)
	fmt.Println(len(v), "violations")
	for i, s := range v {
		if i < 60 {
			fmt.Println(" -", s)
		}
	}
}
