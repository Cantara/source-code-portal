package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docsDir is where the three documents and the server-authored list live: the design directory `20-design/api` next to
// the assets, or $APICHECK_DOCS (in the repository: a directory holding a copy of the single-file form, see design 12.2).
func docsDir(t *testing.T) string {
	if d := os.Getenv("APICHECK_DOCS"); d != "" {
		return d
	}
	return filepath.Join("..", "..", "..", "api")
}

type edit struct{ file, old, new string }

// stage copies the documents into a temporary directory, applies the edits (each `old` must exist; the first occurrence is
// replaced) and returns the argument list of run.
func stage(t *testing.T, publicOnly bool, edits ...edit) []string {
	t.Helper()
	src, dst := docsDir(t), t.TempDir()
	for _, f := range []string{"common.yaml", "openapi-public.yaml", "openapi-admin.yaml", "trusted-members.txt"} {
		b, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range edits {
		p := filepath.Join(dst, e.file)
		b, _ := os.ReadFile(p)
		if !bytes.Contains(b, []byte(e.old)) {
			t.Fatalf("mutation anchor not found in %s: %q", e.file, e.old)
		}
		b = bytes.Replace(b, []byte(e.old), []byte(e.new), 1)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"-trusted", filepath.Join(dst, "trusted-members.txt"), "-public", filepath.Join(dst, "openapi-public.yaml")}
	if !publicOnly {
		args = append(args, "-admin", filepath.Join(dst, "openapi-admin.yaml"))
	}
	return args
}

func runCapture(args []string) (int, string) {
	skipExamples = false
	var buf bytes.Buffer
	out = &buf
	defer func() { out = os.Stdout }()
	code := run(args)
	return code, buf.String()
}

func TestBaselineIsClean(t *testing.T) {
	code, text := runCapture(stage(t, false))
	if code != 0 || !strings.Contains(text, "apicheck: 0 failures") {
		t.Fatalf("the committed documents must pass every rule:\n%s", text)
	}
}

// TestEveryRuleHasANegativeTest mutates the documents so that exactly one rule must fail and asserts that it does.
func TestEveryRuleHasANegativeTest(t *testing.T) {
	pub, com, adm, tru := "openapi-public.yaml", "common.yaml", "openapi-admin.yaml", "trusted-members.txt"
	cases := []struct {
		rule       string
		publicOnly bool
		edits      []edit
	}{
		{"A1", true, []edit{{pub, "example: {version: 1841, builtAt: '2026-10-14T09:41:07Z', readiness: ready, buildId: 9c4e1f2}", "example: {version: 1841, builtAt: '2026-10-14T09:41:07Z', readiness: 7, buildId: 9c4e1f2}"}}},
		{"A2", true, []edit{{pub, "  /api/v1/site:\n", "  /v2/site:\n"}}},
		{"A3", true, []edit{{pub, "operationId: getSite\n", "operationId: get_site\n"}}},
		{"A4", true, []edit{{pub, "tags: [meta]", "tags: [meta, live]"}}},
		{"A5", true, []edit{{pub, "      x-scp-route: api.openapi\n", ""}}},
		{"A5", true, []edit{{pub, "x-scp-etag: body-hash", "x-scp-etag: hash"}}},
		{"A5", true, []edit{{pub, "x-scp-owner: apispec", "x-scp-owner: nobody"}}},
		{"A6", true, []edit{{pub, "      summary: This contract\n", ""}}},
		{"A7", true, []edit{{pub, `              example: "openapi: 3.1.0\ninfo:\n  title: Source Code Portal API\n  version: 1.0.0\n"` + "\n", ""}}},
		{"A8", true, []edit{{pub, "        default: {$ref: 'common.yaml#/components/responses/Problem'}", "        default: {$ref: 'common.yaml#/components/responses/NotFound'}"}}},
		{"A9", true, []edit{{pub, "        '304': {$ref: 'common.yaml#/components/responses/NotModified'}\n", ""}}},
		{"A10", true, []edit{{pub, "        '308': {$ref: 'common.yaml#/components/responses/RepoRenamed'}\n", ""}}},
		{"A1|A11", true, []edit{{pub, "  /api/v1/pages/{slug}:\n", "  /api/v1/pages/{pslug}:\n"}}},
		{"A12", true, []edit{{pub, "        page: {$ref: 'common.yaml#/components/schemas/PageInfo'}\n        freshness: {$ref: 'common.yaml#/components/schemas/Freshness'}\n\n    # ================================================================= docs", "        freshness: {$ref: 'common.yaml#/components/schemas/Freshness'}\n\n    # ================================================================= docs"}}},
		{"A12", true, []edit{{pub, "            Link: {$ref: 'common.yaml#/components/headers/Link'}\n", ""}}},
		{"A13", true, []edit{{com, "limit: {type: integer, minimum: 1, description: The page size that was applied.}", "limit: {type: [integer, string], minimum: 1, description: The page size that was applied.}"}}},
		{"A14", true, []edit{{pub, "        - $ref: '#/components/schemas/GroupSummary'\n        - type: object\n          required: [repos]", "        - $ref: '#/components/schemas/GroupSummary'\n        - $ref: '#/components/schemas/GroupSummary'\n        - type: object\n          required: [repos]"}}},
		{"A15", true, []edit{{com, "fetchedAt: {$ref: '#/components/schemas/NullableTimestamp'}", `fetchedAt: {type: [string, "null"], format: date-time}`}}},
		{"A16", true, []edit{{com, "        nextCursor: {type:", "        next_cursor: {type:"}}},
		{"A17+noex", true, []edit{{com, "required: [nextCursor, limit]", "required: [nextCursor, limit, bogus]"}}},
		{"A18", true, []edit{{pub, "  schemas:\n", "  schemas:\n    Slug: {type: string, minLength: 1, pattern: '^x$'}\n"}}},
		{"A19", true, []edit{{pub, "  schemas:\n", "  schemas:\n    Unused: {type: object, properties: {a: {type: integer}}}\n"}}},
		{"A20", false, []edit{{adm, "  /api/v1/site: {$ref: 'openapi-public.yaml#/paths/~1api~1v1~1site'}\n", ""}}},
		{"A21", true, []edit{{pub, "                repoCount: 212\n", "                repoCount: 212\n                repoCnt: 212\n"}}},
		{"A22", true, []edit{{com, "required: [status, kind, asOf, fetchedAt, checkedAt, staleSince, lastErrorCode]", "required: [status, kind, asOf, fetchedAt, checkedAt, staleSince]"}}},
		{"A23", true, []edit{{com, "schema: {type: string, minLength: 1, maxLength: 200}", "schema: {type: string, maxLength: 200}"}}},
		{"A24", true, []edit{{com, "subject: {type: string, maxLength: 200, x-scp-untrusted: true}", "subject: {type: string, maxLength: 200}"}}},
		{"A24", true, []edit{{tru, "Announcement.id", "Commit.subject\nAnnouncement.id"}}},
		{"A24", false, []edit{{tru, "Announcement.id", "Nope.member\nAnnouncement.id"}}},
		{"A25", true, []edit{{com, "        authorLogin: {type:", "        authorEmail: {type: string, x-scp-untrusted: true}\n        authorLogin: {type:"}}},
		{"A26", true, []edit{{com, "enum: [fresh, stale, error, unknown]", "enum: [fresh, stale, error, Unknown]"}}},
		{"A27", true, []edit{{pub, "servers:\n  - url: /\n", "servers:\n  - url: https://portal.example.com\n"}}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.rule+"/"+strings.ReplaceAll(c.edits[0].new[:min(24, len(c.edits[0].new))], "\n", " "), func(t *testing.T) {
			rule, args := c.rule, stage(t, c.publicOnly, c.edits...)
			if strings.HasSuffix(rule, "+noex") { // the mutation would also invalidate an example, which stops the run at A1
				rule, args = strings.TrimSuffix(rule, "+noex"), append([]string{"-no-examples"}, args...)
			}
			code, text := runCapture(args)
			found := false
			for _, r := range strings.Split(rule, "|") {
				found = found || strings.Contains(text, "FAIL "+r+":")
			}
			if code == 0 || !found {
				t.Fatalf("rule %s did not fail on its mutation (exit %d):\n%s", c.rule, code, text)
			}
		})
	}
}
