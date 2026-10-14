//go:build !race

package render

import "testing"

const timeScale = 1

func heavy(t *testing.T) { t.Helper() }
