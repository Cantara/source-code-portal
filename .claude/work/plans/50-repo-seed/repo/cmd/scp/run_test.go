package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
		out  string // substring of stdout
		err  string // substring of stderr
	}{
		{"no arguments is a usage error", nil, exitUsage, "", "usage: scp"},
		{"help", []string{"help"}, exitOK, "usage: scp", ""},
		{"version", []string{"version"}, exitOK, "scp dev build", ""},
		{"unknown command", []string{"frobnicate"}, exitUsage, "", `unknown command "frobnicate"`},
		{"version with a stray argument", []string{"version", "x"}, exitUsage, "", "unexpected argument"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if got := run(context.Background(), c.args, &out, &errb); got != c.code {
				t.Fatalf("exit code %d, want %d (stderr %q)", got, c.code, errb.String())
			}
			if !strings.Contains(out.String(), c.out) || !strings.Contains(errb.String(), c.err) {
				t.Fatalf("stdout %q, stderr %q", out.String(), errb.String())
			}
		})
	}
}

func TestVersionJSON(t *testing.T) {
	var out, errb bytes.Buffer
	if got := run(context.Background(), []string{"version", "--json"}, &out, &errb); got != exitOK {
		t.Fatalf("exit code %d (stderr %q)", got, errb.String())
	}
	var bi BuildInfo
	if err := json.Unmarshal(out.Bytes(), &bi); err != nil {
		t.Fatal(err)
	}
	if bi.Version != "dev" || bi.SPA != "placeholder" || bi.Go == "" {
		t.Fatalf("unexpected build info %+v", bi)
	}
}
