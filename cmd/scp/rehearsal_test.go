package main

import (
	"os"
	"os/signal"
	"syscall"
	"testing"

	gogh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/github/githubtest"
)

// TestServeForRehearsal serves a fake GitHub (the organisation acme with the repositories alpha and beta, the token rehearsal-token) until
// it is interrupted, and writes its address to the file REHEARSAL_GH_URL_FILE: the GitHub of scripts/upgrade-rehearsal.sh, which runs
// two builds of the program against one store. Without that variable it is skipped.
func TestServeForRehearsal(t *testing.T) {
	out := os.Getenv("REHEARSAL_GH_URL_FILE")
	if out == "" {
		t.Skip("REHEARSAL_GH_URL_FILE not set")
	}
	gh := githubtest.New(t)
	gh.AddToken("rehearsal-token")
	gh.AddRepo("acme", gogh.Repository{Name: new("alpha"), Visibility: new("public")})
	gh.AddRepo("acme", gogh.Repository{Name: new("beta"), Visibility: new("public")})
	if err := os.WriteFile(out, []byte(gh.BaseURL()), 0o600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}
