package main

import (
	"slices"

	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/platform/prereq"
)

// needs is what the switched-on features need of GitHub.
func needs(f config.Features) []prereq.Need {
	return slices.DeleteFunc(slices.Clone(prereq.Needs), func(n prereq.Need) bool {
		return n.Feature == prereq.Pulls && !f.Pulls || n.Feature == prereq.Issues && !f.Issues
	})
}

// offElements is the layout elements of the features switched off.
func offElements(f config.Features) []oapi.LayoutElementKind {
	var off []oapi.LayoutElementKind
	if !f.Pulls {
		off = append(off, oapi.LayoutElementKindPulls)
	}
	if !f.Issues {
		off = append(off, oapi.LayoutElementKindIssues)
	}
	return off
}
