package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/cantara/source-code-portal/internal/health"
)

// rulesFixture is the web tests' copy of GET /api/v1/rules; SCP_UPDATE_RULES=1 rewrites it.
const rulesFixture = "../../web/src/test/rules.json"

// TestRulesFixture holds the web tests to the rules the server tells, so a change of a threshold or a word fails here until the
// fixture is rewritten and the web tests pass with it.
func TestRulesFixture(t *testing.T) {
	want, err := json.MarshalIndent(rulesOf(health.Describe()), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if os.Getenv("SCP_UPDATE_RULES") != "" {
		if err := os.WriteFile(rulesFixture, want, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(rulesFixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the server's rules: run SCP_UPDATE_RULES=1 go test -run TestRulesFixture ./cmd/scp", rulesFixture)
	}
}
